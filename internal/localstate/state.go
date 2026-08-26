// Package localstate owns provider-local configuration. Relay-facing models
// must never include values from LocalAgent.Repository.
package localstate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/zalando/go-keyring"
)

const keyringService = "peerctx"

type Profile struct {
	ProjectID       string `json:"project_id"`
	ProjectName     string `json:"project_name"`
	RelayURL        string `json:"relay_url"`
	MemberID        string `json:"member_id"`
	MemberName      string `json:"member_name"`
	CredentialID    string `json:"credential_id,omitempty"`
	CredentialStore string `json:"credential_store"`
	CredentialRef   string `json:"credential_ref"`
}

type LocalAgent struct {
	AgentID    string `json:"agent_id"`
	ProjectID  string `json:"project_id"`
	Repository string `json:"repository"`
}

type State struct {
	SchemaVersion    int                   `json:"schema_version"`
	CurrentProjectID string                `json:"current_project_id,omitempty"`
	Profiles         map[string]Profile    `json:"profiles"`
	Agents           map[string]LocalAgent `json:"agents"`
}

type Manager struct {
	dir       string
	statePath string
}

func NewManager(dir string) (*Manager, error) {
	if dir == "" {
		var err error
		dir, err = os.UserConfigDir()
		if err != nil {
			return nil, err
		}
		dir = filepath.Join(dir, "peerctx")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	return &Manager{dir: abs, statePath: filepath.Join(abs, "state.json")}, nil
}

func DefaultManager() (*Manager, error) { return NewManager(os.Getenv("PEERCTX_CONFIG_DIR")) }

// Directory is the provider-local PeerContext state root. It must never be
// sent to Relay or copied into public Agent manifests.
func (m *Manager) Directory() string { return m.dir }

func (m *Manager) Load() (State, error) {
	data, err := os.ReadFile(m.statePath)
	if errors.Is(err, os.ErrNotExist) {
		return emptyState(), nil
	}
	if err != nil {
		return State{}, err
	}
	var state State
	if err = json.Unmarshal(data, &state); err != nil {
		return State{}, fmt.Errorf("decode local state: %w", err)
	}
	if state.SchemaVersion != 1 {
		return State{}, fmt.Errorf("unsupported local state schema_version %d", state.SchemaVersion)
	}
	if state.Profiles == nil {
		state.Profiles = make(map[string]Profile)
	}
	if state.Agents == nil {
		state.Agents = make(map[string]LocalAgent)
	}
	return state, nil
}

func (m *Manager) Save(state State) error {
	state.SchemaVersion = 1
	if state.Profiles == nil {
		state.Profiles = make(map[string]Profile)
	}
	if state.Agents == nil {
		state.Agents = make(map[string]LocalAgent)
	}
	if err := os.MkdirAll(m.dir, 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(m.dir, "state-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err = tmp.Chmod(0600); err == nil {
		_, err = tmp.Write(data)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmpName, m.statePath)
}

func (m *Manager) Profiles() (State, []Profile, error) {
	state, err := m.Load()
	if err != nil {
		return State{}, nil, err
	}
	profiles := make([]Profile, 0, len(state.Profiles))
	for _, profile := range state.Profiles {
		profiles = append(profiles, profile)
	}
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].ProjectName < profiles[j].ProjectName })
	return state, profiles, nil
}

func (m *Manager) Current() (State, Profile, error) {
	state, err := m.Load()
	if err != nil {
		return State{}, Profile{}, err
	}
	if state.CurrentProjectID == "" {
		return State{}, Profile{}, errors.New("no current project; create, join, or use a project first")
	}
	profile, ok := state.Profiles[state.CurrentProjectID]
	if !ok {
		return State{}, Profile{}, errors.New("current project profile is missing")
	}
	return state, profile, nil
}

func (m *Manager) PutProfile(profile Profile, token, explicitFile string) error {
	state, err := m.Load()
	if err != nil {
		return err
	}
	if explicitFile != "" {
		ref, err := createCredentialFile(explicitFile, token)
		if err != nil {
			return err
		}
		profile.CredentialStore = "file"
		profile.CredentialRef = ref
	} else {
		ref := profile.ProjectID + ":" + profile.MemberID
		if err := keyring.Set(keyringService, ref, token); err != nil {
			return fmt.Errorf("store credential in system keychain: %w; use --credential-file only if you explicitly accept a 0600 file", err)
		}
		profile.CredentialStore = "keyring"
		profile.CredentialRef = ref
	}
	state.Profiles[profile.ProjectID] = profile
	state.CurrentProjectID = profile.ProjectID
	return m.Save(state)
}

// PrepareCredentialStore checks the selected backend before a Project is
// created or a one-time invite is consumed. This avoids losing the only usable
// credential after the Relay mutation has already committed.
func (m *Manager) PrepareCredentialStore(explicitFile string) error {
	if explicitFile != "" {
		abs, err := filepath.Abs(explicitFile)
		if err != nil {
			return err
		}
		if _, err := os.Stat(abs); err == nil {
			return errors.New("credential file already exists")
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(abs), 0700); err != nil {
			return err
		}
		test, err := os.CreateTemp(filepath.Dir(abs), ".peerctx-credential-preflight-*")
		if err != nil {
			return err
		}
		name := test.Name()
		if closeErr := test.Close(); closeErr != nil {
			_ = os.Remove(name)
			return closeErr
		}
		return os.Remove(name)
	}
	ref := "preflight:" + strconv.FormatInt(time.Now().UnixNano(), 10)
	if err := keyring.Set(keyringService, ref, "peerctx-keychain-preflight"); err != nil {
		return fmt.Errorf("system keychain is unavailable: %w; use --credential-file only if you explicitly accept a 0600 file", err)
	}
	if err := keyring.Delete(keyringService, ref); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return err
	}
	return nil
}

func (m *Manager) Use(projectID string) error {
	state, err := m.Load()
	if err != nil {
		return err
	}
	if _, ok := state.Profiles[projectID]; !ok {
		return errors.New("project is not configured locally")
	}
	state.CurrentProjectID = projectID
	return m.Save(state)
}

func (m *Manager) Token(profile Profile) (string, error) {
	switch profile.CredentialStore {
	case "keyring":
		token, err := keyring.Get(keyringService, profile.CredentialRef)
		if err != nil {
			return "", fmt.Errorf("read credential from system keychain: %w", err)
		}
		return token, nil
	case "file":
		data, err := os.ReadFile(profile.CredentialRef)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(data)), nil
	default:
		return "", errors.New("unsupported credential store")
	}
}

func (m *Manager) ReplaceToken(profile Profile, token string) error {
	switch profile.CredentialStore {
	case "keyring":
		return keyring.Set(keyringService, profile.CredentialRef, token)
	case "file":
		_, err := writeCredentialFile(profile.CredentialRef, token)
		return err
	default:
		return errors.New("unsupported credential store")
	}
}

func (m *Manager) DeleteToken(profile Profile) error {
	switch profile.CredentialStore {
	case "keyring":
		err := keyring.Delete(keyringService, profile.CredentialRef)
		if errors.Is(err, keyring.ErrNotFound) {
			return nil
		}
		return err
	case "file":
		return os.Remove(profile.CredentialRef)
	default:
		return errors.New("unsupported credential store")
	}
}

func (m *Manager) PutAgent(agent LocalAgent) error {
	if strings.TrimSpace(agent.Repository) == "" {
		return errors.New("repository path is required")
	}
	abs, err := filepath.Abs(agent.Repository)
	if err != nil {
		return err
	}
	agent.Repository = abs
	state, err := m.Load()
	if err != nil {
		return err
	}
	state.Agents[agent.AgentID] = agent
	return m.Save(state)
}
func (m *Manager) Agent(projectID, agentID string) (LocalAgent, error) {
	state, err := m.Load()
	if err != nil {
		return LocalAgent{}, err
	}
	agent, ok := state.Agents[agentID]
	if !ok || agent.ProjectID != projectID {
		return LocalAgent{}, errors.New("agent repository is not configured on this machine")
	}
	return agent, nil
}

func emptyState() State {
	return State{SchemaVersion: 1, Profiles: make(map[string]Profile), Agents: make(map[string]LocalAgent)}
}
func writeCredentialFile(path, token string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if err = os.MkdirAll(filepath.Dir(abs), 0700); err != nil {
		return "", err
	}
	if err = os.WriteFile(abs, []byte(token+"\n"), 0600); err != nil {
		return "", err
	}
	if err = os.Chmod(abs, 0600); err != nil {
		return "", err
	}
	return abs, nil
}

func createCredentialFile(path, token string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if err = os.MkdirAll(filepath.Dir(abs), 0700); err != nil {
		return "", err
	}
	file, err := os.OpenFile(abs, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	_, writeErr := file.WriteString(token + "\n")
	closeErr := file.Close()
	if writeErr != nil {
		_ = os.Remove(abs)
		return "", writeErr
	}
	if closeErr != nil {
		_ = os.Remove(abs)
		return "", closeErr
	}
	return abs, nil
}

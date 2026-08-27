// Package v2state owns LAN-first local state. It intentionally uses a v2
// directory so legacy Relay profiles remain untouched.
package v2state

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/zalando/go-keyring"
)

const keyringService = "peerctx-v2"

type Profile struct {
	ProjectID       string   `json:"project_id"`
	ProjectName     string   `json:"project_name"`
	MemberID        string   `json:"member_id"`
	MemberName      string   `json:"member_name"`
	Hosted          bool     `json:"hosted"`
	HostPublicKey   []byte   `json:"host_public_key"`
	Endpoints       []string `json:"endpoints"`
	PrivateKeyStore string   `json:"private_key_store"`
}

type LocalAgent struct {
	AgentID    string `json:"agent_id"`
	ProjectID  string `json:"project_id"`
	Repository string `json:"repository"`
}

type State struct {
	SchemaVersion    int                   `json:"schema_version"`
	CurrentProjectID string                `json:"current_project_id,omitempty"`
	ListenPort       int                   `json:"listen_port,omitempty"`
	Profiles         map[string]Profile    `json:"profiles"`
	Agents           map[string]LocalAgent `json:"agents"`
}

type Manager struct {
	dir       string
	statePath string
	mu        sync.RWMutex
}

func DefaultManager() (*Manager, error) {
	root := os.Getenv("PEERCTX_CONFIG_DIR")
	if root == "" {
		var err error
		root, err = os.UserConfigDir()
		if err != nil {
			return nil, err
		}
		root = filepath.Join(root, "peerctx")
	}
	return NewManager(filepath.Join(root, "v2"))
}

func NewManager(dir string) (*Manager, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	return &Manager{dir: abs, statePath: filepath.Join(abs, "state.json")}, nil
}

func (m *Manager) Directory() string    { return m.dir }
func (m *Manager) SocketPath() string   { return filepath.Join(m.dir, "service.sock") }
func (m *Manager) DatabasePath() string { return filepath.Join(m.dir, "host.sqlite") }
func (m *Manager) LogPath() string      { return filepath.Join(m.dir, "service.log") }

func emptyState() State {
	return State{SchemaVersion: 2, Profiles: map[string]Profile{}, Agents: map[string]LocalAgent{}}
}

func (m *Manager) Load() (State, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.loadUnlocked()
}

func (m *Manager) loadUnlocked() (State, error) {
	data, err := os.ReadFile(m.statePath)
	if errors.Is(err, os.ErrNotExist) {
		return emptyState(), nil
	}
	if err != nil {
		return State{}, err
	}
	var state State
	if err = json.Unmarshal(data, &state); err != nil {
		return State{}, fmt.Errorf("decode v2 state: %w", err)
	}
	if state.SchemaVersion != 2 {
		return State{}, fmt.Errorf("unsupported v2 state schema_version %d", state.SchemaVersion)
	}
	if state.Profiles == nil {
		state.Profiles = map[string]Profile{}
	}
	if state.Agents == nil {
		state.Agents = map[string]LocalAgent{}
	}
	return state, nil
}

func (m *Manager) Save(state State) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.saveUnlocked(state)
}

func (m *Manager) saveUnlocked(state State) error {
	state.SchemaVersion = 2
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
	name := tmp.Name()
	defer os.Remove(name)
	if err = tmp.Chmod(0600); err == nil {
		_, err = tmp.Write(data)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, m.statePath)
}

func (m *Manager) PutProfile(profile Profile, privateKey ed25519.PrivateKey) error {
	if len(privateKey) != ed25519.PrivateKeySize {
		return errors.New("invalid Project private key")
	}
	if err := m.storePrivateKey(profile.ProjectID, privateKey); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	state, err := m.loadUnlocked()
	if err != nil {
		return err
	}
	profile.PrivateKeyStore = "keyring"
	if os.Getenv("PEERCTX_TEST_KEY_DIR") != "" {
		profile.PrivateKeyStore = "test-file"
	}
	state.Profiles[profile.ProjectID] = profile
	state.CurrentProjectID = profile.ProjectID
	return m.saveUnlocked(state)
}

func (m *Manager) Current() (State, Profile, error) {
	state, err := m.Load()
	if err != nil {
		return State{}, Profile{}, err
	}
	profile, ok := state.Profiles[state.CurrentProjectID]
	if !ok {
		if _, legacyErr := os.Stat(filepath.Join(filepath.Dir(m.dir), "state.json")); legacyErr == nil {
			return State{}, Profile{}, errors.New("a PeerContext 0.1.1 Relay profile was found, but LAN protocol v2 is intentionally incompatible; create or join a new LAN Project")
		}
		return State{}, Profile{}, errors.New("no current LAN v2 Project; create, join, or use one first")
	}
	return state, profile, nil
}

func (m *Manager) Use(projectID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, err := m.loadUnlocked()
	if err != nil {
		return err
	}
	if _, ok := state.Profiles[projectID]; !ok {
		return errors.New("Project is not configured locally")
	}
	state.CurrentProjectID = projectID
	return m.saveUnlocked(state)
}

func (m *Manager) Profiles() ([]Profile, error) {
	state, err := m.Load()
	if err != nil {
		return nil, err
	}
	items := make([]Profile, 0, len(state.Profiles))
	for _, profile := range state.Profiles {
		items = append(items, profile)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ProjectName < items[j].ProjectName })
	return items, nil
}

func (m *Manager) Profile(projectID string) (Profile, error) {
	state, err := m.Load()
	if err != nil {
		return Profile{}, err
	}
	profile, ok := state.Profiles[projectID]
	if !ok {
		return Profile{}, errors.New("Project is not configured locally")
	}
	return profile, nil
}

func (m *Manager) LocalAgents() ([]LocalAgent, error) {
	state, err := m.Load()
	if err != nil {
		return nil, err
	}
	items := make([]LocalAgent, 0, len(state.Agents))
	for _, agent := range state.Agents {
		items = append(items, agent)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].AgentID < items[j].AgentID })
	return items, nil
}

func (m *Manager) PrivateKey(projectID string) (ed25519.PrivateKey, error) {
	encoded, err := m.loadPrivateKey(projectID)
	if err != nil {
		return nil, err
	}
	data, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(data) != ed25519.PrivateKeySize {
		return nil, errors.New("stored Project private key is invalid")
	}
	return ed25519.PrivateKey(data), nil
}

func (m *Manager) PutAgent(agent LocalAgent) error {
	abs, err := filepath.Abs(agent.Repository)
	if err != nil {
		return err
	}
	agent.Repository = abs
	m.mu.Lock()
	defer m.mu.Unlock()
	state, err := m.loadUnlocked()
	if err != nil {
		return err
	}
	state.Agents[agent.AgentID] = agent
	return m.saveUnlocked(state)
}

func (m *Manager) RemoveAgent(agentID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, err := m.loadUnlocked()
	if err != nil {
		return err
	}
	delete(state.Agents, agentID)
	return m.saveUnlocked(state)
}

func (m *Manager) RemoveProfile(projectID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, err := m.loadUnlocked()
	if err != nil {
		return err
	}
	delete(state.Profiles, projectID)
	for agentID, agent := range state.Agents {
		if agent.ProjectID == projectID {
			delete(state.Agents, agentID)
		}
	}
	if state.CurrentProjectID == projectID {
		state.CurrentProjectID = ""
	}
	if err := m.deletePrivateKey(projectID); err != nil && !errors.Is(err, keyring.ErrNotFound) && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return m.saveUnlocked(state)
}

func (m *Manager) SetListenPort(port int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, err := m.loadUnlocked()
	if err != nil {
		return err
	}
	state.ListenPort = port
	return m.saveUnlocked(state)
}

func (m *Manager) UpdateEndpoints(projectID string, endpoints []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, err := m.loadUnlocked()
	if err != nil {
		return err
	}
	profile, ok := state.Profiles[projectID]
	if !ok {
		return errors.New("Project is not configured locally")
	}
	profile.Endpoints = append([]string(nil), endpoints...)
	state.Profiles[projectID] = profile
	return m.saveUnlocked(state)
}

func (m *Manager) storePrivateKey(projectID string, key ed25519.PrivateKey) error {
	value := base64.RawURLEncoding.EncodeToString(key)
	if dir := os.Getenv("PEERCTX_TEST_KEY_DIR"); dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, m.testKeyName(projectID)), []byte(value), 0600)
	}
	return keyring.Set(keyringService, projectID, value)
}

func (m *Manager) loadPrivateKey(projectID string) (string, error) {
	if dir := os.Getenv("PEERCTX_TEST_KEY_DIR"); dir != "" {
		data, err := os.ReadFile(filepath.Join(dir, m.testKeyName(projectID)))
		return strings.TrimSpace(string(data)), err
	}
	return keyring.Get(keyringService, projectID)
}

func (m *Manager) deletePrivateKey(projectID string) error {
	if dir := os.Getenv("PEERCTX_TEST_KEY_DIR"); dir != "" {
		return os.Remove(filepath.Join(dir, m.testKeyName(projectID)))
	}
	return keyring.Delete(keyringService, projectID)
}

func (m *Manager) testKeyName(projectID string) string {
	sum := sha256.Sum256([]byte(m.dir))
	return base64.RawURLEncoding.EncodeToString(sum[:9]) + "-" + safeKeyName(projectID)
}

func safeKeyName(value string) string {
	value = strings.ReplaceAll(value, "/", "_")
	value = strings.ReplaceAll(value, "..", "_")
	return value + ".key"
}

// Package worktree owns provider-local detached Git worktrees. It shells out
// to Git for lifecycle operations but never opens repository files or diffs.
package worktree

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	protocolv1 "github.com/wWzZb/peercontext/pkg/protocol/v1"
)

type Error struct {
	Code string
	Err  error
}

func (e *Error) Error() string {
	if e.Err == nil {
		return e.Code
	}
	return e.Code + ": " + e.Err.Error()
}
func (e *Error) Unwrap() error { return e.Err }

type Record struct {
	SchemaVersion int       `json:"schema_version"`
	ID            string    `json:"worktree_id"`
	AgentID       string    `json:"agent_id"`
	RequestID     string    `json:"request_id"`
	Repository    string    `json:"repository"`
	Path          string    `json:"path"`
	GitCommonDir  string    `json:"git_common_dir"`
	BaseCommit    string    `json:"base_commit"`
	CreatedAt     time.Time `json:"created_at"`
}

func (r Record) Public() protocolv1.WorktreeResult {
	return protocolv1.WorktreeResult{SchemaVersion: 1, ID: r.ID, AgentID: r.AgentID, RequestID: r.RequestID, BaseCommit: r.BaseCommit}
}

type Manager struct {
	root      string
	records   string
	checkouts string
	gitPath   string
}

var gitLifecycleMu sync.Mutex

func New(root string) (*Manager, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("worktree state root is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return nil, &Error{Code: "git_unavailable", Err: err}
	}
	base := filepath.Join(abs, "worktrees")
	return &Manager{root: base, records: filepath.Join(base, "records"), checkouts: filepath.Join(base, "checkouts"), gitPath: gitPath}, nil
}

func (m *Manager) Create(repository, agentID, requestID, baseCommit string) (record Record, err error) {
	gitLifecycleMu.Lock()
	defer gitLifecycleMu.Unlock()
	if strings.TrimSpace(agentID) == "" || strings.TrimSpace(requestID) == "" {
		return Record{}, &Error{Code: "worktree_identity_invalid"}
	}
	baseCommit = strings.TrimSpace(baseCommit)
	if baseCommit == "" || len(baseCommit) > 256 || strings.HasPrefix(baseCommit, "-") || strings.ContainsAny(baseCommit, "\r\n\x00") {
		return Record{}, &Error{Code: "base_commit_invalid"}
	}
	repository, err = filepath.EvalSymlinks(repository)
	if err != nil {
		return Record{}, &Error{Code: "agent_repository_unavailable", Err: err}
	}
	repository, err = filepath.Abs(repository)
	if err != nil {
		return Record{}, &Error{Code: "agent_repository_unavailable", Err: err}
	}
	top, err := m.git(repository, "rev-parse", "--show-toplevel")
	if err != nil {
		return Record{}, &Error{Code: "agent_repository_unavailable", Err: err}
	}
	if normalized, normalizeErr := filepath.EvalSymlinks(strings.TrimSpace(top)); normalizeErr == nil {
		repository = normalized
	}
	resolved, err := m.git(repository, "rev-parse", "--verify", "--end-of-options", baseCommit+"^{commit}")
	if err != nil {
		return Record{}, &Error{Code: "base_commit_invalid", Err: err}
	}
	resolved = strings.TrimSpace(resolved)
	id, err := newID()
	if err != nil {
		return Record{}, &Error{Code: "worktree_create_failed", Err: err}
	}
	if err = os.MkdirAll(m.records, 0700); err != nil {
		return Record{}, &Error{Code: "worktree_create_failed", Err: err}
	}
	if err = os.MkdirAll(m.checkouts, 0700); err != nil {
		return Record{}, &Error{Code: "worktree_create_failed", Err: err}
	}
	path := filepath.Join(m.checkouts, id)
	if _, err = m.git(repository, "worktree", "add", "--detach", path, resolved); err != nil {
		return Record{}, &Error{Code: "worktree_create_failed", Err: err}
	}
	created := true
	defer func() {
		if err != nil && created {
			_, _ = m.git(repository, "worktree", "remove", "--force", path)
		}
	}()
	head, err := m.git(path, "rev-parse", "HEAD")
	if err != nil || strings.TrimSpace(head) != resolved {
		return Record{}, &Error{Code: "worktree_verification_failed", Err: err}
	}
	if _, symbolicErr := m.git(path, "symbolic-ref", "-q", "HEAD"); symbolicErr == nil {
		return Record{}, &Error{Code: "worktree_not_detached"}
	}
	common, err := m.git(path, "rev-parse", "--git-common-dir")
	if err != nil {
		return Record{}, &Error{Code: "worktree_verification_failed", Err: err}
	}
	common = strings.TrimSpace(common)
	if !filepath.IsAbs(common) {
		common = filepath.Join(path, common)
	}
	common, err = filepath.Abs(common)
	if err != nil {
		return Record{}, &Error{Code: "worktree_verification_failed", Err: err}
	}
	record = Record{SchemaVersion: 1, ID: id, AgentID: agentID, RequestID: requestID, Repository: repository, Path: path, GitCommonDir: common, BaseCommit: resolved, CreatedAt: time.Now().UTC()}
	if err = m.save(record); err != nil {
		return Record{}, &Error{Code: "worktree_record_failed", Err: err}
	}
	created = false
	return record, nil
}

func (m *Manager) List() ([]Record, error) {
	entries, err := os.ReadDir(m.records)
	if errors.Is(err, os.ErrNotExist) {
		return []Record{}, nil
	}
	if err != nil {
		return nil, err
	}
	records := make([]Record, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		record, err := m.load(strings.TrimSuffix(entry.Name(), ".json"))
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].CreatedAt.Before(records[j].CreatedAt) })
	return records, nil
}

func (m *Manager) Validate(record Record) error {
	if record.SchemaVersion != 1 || record.ID == "" || record.Path == "" || record.Repository == "" || record.GitCommonDir == "" {
		return &Error{Code: "worktree_record_invalid"}
	}
	if info, err := os.Stat(record.Path); err != nil || !info.IsDir() {
		return &Error{Code: "worktree_unavailable", Err: err}
	}
	head, err := m.git(record.Path, "rev-parse", "HEAD")
	if err != nil || strings.TrimSpace(head) != record.BaseCommit {
		return &Error{Code: "worktree_base_changed", Err: err}
	}
	if _, err := m.git(record.Path, "symbolic-ref", "-q", "HEAD"); err == nil {
		return &Error{Code: "worktree_not_detached"}
	}
	common, err := m.git(record.Path, "rev-parse", "--git-common-dir")
	if err != nil {
		return &Error{Code: "worktree_verification_failed", Err: err}
	}
	common = strings.TrimSpace(common)
	if !filepath.IsAbs(common) {
		common = filepath.Join(record.Path, common)
	}
	common, _ = filepath.Abs(common)
	if filepath.Clean(common) != filepath.Clean(record.GitCommonDir) {
		return &Error{Code: "git_metadata_boundary_invalid"}
	}
	return nil
}

func (m *Manager) Remove(id string) (Record, error) {
	gitLifecycleMu.Lock()
	defer gitLifecycleMu.Unlock()
	record, err := m.load(id)
	if err != nil {
		return Record{}, err
	}
	if _, err = m.git(record.Repository, "worktree", "remove", "--force", record.Path); err != nil {
		return Record{}, &Error{Code: "worktree_remove_failed", Err: err}
	}
	if err = os.Remove(m.recordPath(record.ID)); err != nil {
		return Record{}, &Error{Code: "worktree_record_failed", Err: err}
	}
	return record, nil
}

func (m *Manager) save(record Record) error {
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	path := m.recordPath(record.ID)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err = file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return err
	}
	return file.Close()
}

func (m *Manager) load(id string) (Record, error) {
	if strings.TrimSpace(id) == "" || strings.ContainsAny(id, `/\\`) {
		return Record{}, &Error{Code: "worktree_id_invalid"}
	}
	data, err := os.ReadFile(m.recordPath(id))
	if errors.Is(err, os.ErrNotExist) {
		return Record{}, &Error{Code: "worktree_not_found", Err: err}
	}
	if err != nil {
		return Record{}, err
	}
	var record Record
	if err = json.Unmarshal(data, &record); err != nil {
		return Record{}, err
	}
	if record.SchemaVersion != 1 || record.ID != id {
		return Record{}, &Error{Code: "worktree_record_invalid"}
	}
	return record, nil
}

func (m *Manager) recordPath(id string) string { return filepath.Join(m.records, id+".json") }

func (m *Manager) git(repository string, args ...string) (string, error) {
	commandArgs := append([]string{"-C", repository}, args...)
	output, err := exec.Command(m.gitPath, commandArgs...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git command failed: %w", err)
	}
	return string(output), nil
}

func newID() (string, error) {
	raw := make([]byte, 18)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "wt_" + base64.RawURLEncoding.EncodeToString(raw), nil
}

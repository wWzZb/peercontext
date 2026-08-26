package relay

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	protocolv1 "github.com/wWzZb/peercontext/pkg/protocol/v1"
	_ "modernc.org/sqlite"
)

var (
	ErrNotFound        = errors.New("not found")
	ErrConflict        = errors.New("conflict")
	ErrUnauthenticated = errors.New("unauthenticated")
	ErrForbidden       = errors.New("forbidden")
	ErrInviteExpired   = errors.New("invite expired")
	ErrInviteConsumed  = errors.New("invite consumed")
	ErrLastOwner       = errors.New("cannot remove the last owner")
	ErrReplayMismatch  = errors.New("request ID replay does not match original request")
)

type Project struct {
	SchemaVersion int       `json:"schema_version"`
	ID            string    `json:"project_id"`
	Name          string    `json:"name"`
	CreatedAt     time.Time `json:"created_at"`
}

type Member struct {
	SchemaVersion int       `json:"schema_version"`
	ID            string    `json:"member_id"`
	ProjectID     string    `json:"project_id"`
	Name          string    `json:"name"`
	Owner         bool      `json:"owner"`
	CreatedAt     time.Time `json:"created_at"`
}

type Credential struct {
	SchemaVersion int        `json:"schema_version"`
	ID            string     `json:"credential_id"`
	MemberID      string     `json:"member_id"`
	CreatedAt     time.Time  `json:"created_at"`
	RevokedAt     *time.Time `json:"revoked_at,omitempty"`
}

type Invite struct {
	SchemaVersion int        `json:"schema_version"`
	ID            string     `json:"invite_id"`
	ProjectID     string     `json:"project_id"`
	CreatedBy     string     `json:"created_by_member_id"`
	ExpiresAt     time.Time  `json:"expires_at"`
	ConsumedAt    *time.Time `json:"consumed_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

type Agent struct {
	SchemaVersion int                      `json:"schema_version"`
	ID            string                   `json:"agent_id"`
	ProjectID     string                   `json:"project_id"`
	OwnerMemberID string                   `json:"owner_member_id"`
	Manifest      protocolv1.AgentManifest `json:"manifest"`
	Online        bool                     `json:"online"`
	UpdatedAt     time.Time                `json:"updated_at"`
}

type ACL struct {
	SchemaVersion int                      `json:"schema_version"`
	AgentID       string                   `json:"agent_id"`
	MemberID      string                   `json:"member_id"`
	Modes         []protocolv1.RequestMode `json:"modes"`
}

type Principal struct {
	Project    Project
	Member     Member
	Credential Credential
}

// Store persists Relay metadata only. Its schema deliberately has no request
// body, answer, bearer token, invite token, Authorization header, or local
// repository path column.
type Store struct {
	db     *sql.DB
	logger *slog.Logger
	now    func() time.Time
}

func OpenStore(path string, logger *slog.Logger) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("database path is required")
	}
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(discardWriter{}, nil))
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	store := &Store{db: db, logger: logger, now: func() time.Time { return time.Now().UTC() }}
	if err := store.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.Exec(`UPDATE request_metadata SET status='failed',updated_at=? WHERE status IN ('running','pending_approval')`, formatTime(store.now())); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("recover incomplete requests: %w", err)
	}
	return store, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	const schema = `
PRAGMA foreign_keys = ON;
CREATE TABLE IF NOT EXISTS projects (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS members (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  is_owner INTEGER NOT NULL CHECK (is_owner IN (0, 1)),
  created_at TEXT NOT NULL,
  UNIQUE(project_id, name)
);
CREATE TABLE IF NOT EXISTS credentials (
  id TEXT PRIMARY KEY,
  member_id TEXT NOT NULL REFERENCES members(id) ON DELETE CASCADE,
  token_hash TEXT NOT NULL UNIQUE,
  created_at TEXT NOT NULL,
  revoked_at TEXT
);
CREATE TABLE IF NOT EXISTS invites (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  created_by_member_id TEXT REFERENCES members(id) ON DELETE SET NULL,
  token_hash TEXT NOT NULL UNIQUE,
  expires_at TEXT NOT NULL,
  consumed_at TEXT,
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS agents (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  owner_member_id TEXT NOT NULL REFERENCES members(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  manifest_json TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(project_id, name)
);
CREATE TABLE IF NOT EXISTS agent_acl (
  agent_id TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
  member_id TEXT NOT NULL REFERENCES members(id) ON DELETE CASCADE,
  mode TEXT NOT NULL CHECK (mode IN ('read', 'write')),
  created_at TEXT NOT NULL,
  PRIMARY KEY(agent_id, member_id, mode)
);
CREATE TABLE IF NOT EXISTS request_metadata (
  request_id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  requester_member_id TEXT NOT NULL,
  agent_id TEXT NOT NULL,
  mode TEXT NOT NULL CHECK (mode IN ('read', 'write')),
  status TEXT NOT NULL,
  body_bytes INTEGER NOT NULL,
  body_sha256 TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);`
	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("migrate relay metadata: %w", err)
	}
	return nil
}

func (s *Store) CreateProject(ctx context.Context, projectName, ownerName string) (Project, Member, string, error) {
	if strings.TrimSpace(projectName) == "" || strings.TrimSpace(ownerName) == "" {
		return Project{}, Member{}, "", errors.New("project and owner names are required")
	}
	now := s.now()
	project := Project{SchemaVersion: 1, ID: newID("prj"), Name: strings.TrimSpace(projectName), CreatedAt: now}
	owner := Member{SchemaVersion: 1, ID: newID("mem"), ProjectID: project.ID, Name: strings.TrimSpace(ownerName), Owner: true, CreatedAt: now}
	credentialID, token, tokenHash, err := newSecret("cred")
	if err != nil {
		return Project{}, Member{}, "", err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Project{}, Member{}, "", err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO projects(id,name,created_at) VALUES(?,?,?)`, project.ID, project.Name, formatTime(now)); err != nil {
		return Project{}, Member{}, "", mapConflict(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO members(id,project_id,name,is_owner,created_at) VALUES(?,?,?,?,?)`, owner.ID, owner.ProjectID, owner.Name, 1, formatTime(now)); err != nil {
		return Project{}, Member{}, "", mapConflict(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO credentials(id,member_id,token_hash,created_at) VALUES(?,?,?,?)`, credentialID, owner.ID, tokenHash, formatTime(now)); err != nil {
		return Project{}, Member{}, "", err
	}
	if err = tx.Commit(); err != nil {
		return Project{}, Member{}, "", err
	}
	return project, owner, token, nil
}

func (s *Store) Authenticate(ctx context.Context, token string) (Principal, error) {
	if token == "" {
		return Principal{}, ErrUnauthenticated
	}
	row := s.db.QueryRowContext(ctx, `
SELECT p.id,p.name,p.created_at,m.id,m.project_id,m.name,m.is_owner,m.created_at,c.id,c.created_at,c.revoked_at
FROM credentials c JOIN members m ON m.id=c.member_id JOIN projects p ON p.id=m.project_id
WHERE c.token_hash=? AND c.revoked_at IS NULL`, secretHash(token))
	var p Principal
	var projectCreated, memberCreated, credentialCreated string
	var owner int
	var revoked sql.NullString
	if err := row.Scan(&p.Project.ID, &p.Project.Name, &projectCreated, &p.Member.ID, &p.Member.ProjectID, &p.Member.Name, &owner, &memberCreated, &p.Credential.ID, &credentialCreated, &revoked); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Principal{}, ErrUnauthenticated
		}
		return Principal{}, err
	}
	p.Project.SchemaVersion, p.Member.SchemaVersion, p.Credential.SchemaVersion = 1, 1, 1
	p.Member.Owner = owner == 1
	p.Project.CreatedAt, _ = parseTime(projectCreated)
	p.Member.CreatedAt, _ = parseTime(memberCreated)
	p.Credential.CreatedAt, _ = parseTime(credentialCreated)
	p.Credential.MemberID = p.Member.ID
	return p, nil
}

func (s *Store) CreateInvite(ctx context.Context, principal Principal, ttl time.Duration) (Invite, string, error) {
	if !principal.Member.Owner {
		return Invite{}, "", ErrForbidden
	}
	if ttl <= 0 {
		return Invite{}, "", errors.New("invite ttl must be positive")
	}
	now := s.now()
	id, token, hash, err := newSecret("inv")
	if err != nil {
		return Invite{}, "", err
	}
	invite := Invite{SchemaVersion: 1, ID: id, ProjectID: principal.Project.ID, CreatedBy: principal.Member.ID, ExpiresAt: now.Add(ttl), CreatedAt: now}
	_, err = s.db.ExecContext(ctx, `INSERT INTO invites(id,project_id,created_by_member_id,token_hash,expires_at,created_at) VALUES(?,?,?,?,?,?)`, invite.ID, invite.ProjectID, invite.CreatedBy, hash, formatTime(invite.ExpiresAt), formatTime(now))
	if err != nil {
		return Invite{}, "", err
	}
	return invite, token, nil
}

func (s *Store) JoinInvite(ctx context.Context, token, memberName string) (Project, Member, string, error) {
	if token == "" {
		return Project{}, Member{}, "", ErrUnauthenticated
	}
	if strings.TrimSpace(memberName) == "" {
		return Project{}, Member{}, "", errors.New("member name is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Project{}, Member{}, "", err
	}
	defer tx.Rollback()
	var inviteID, projectID, expires string
	var consumed sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT id,project_id,expires_at,consumed_at FROM invites WHERE token_hash=?`, secretHash(token)).Scan(&inviteID, &projectID, &expires, &consumed)
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, Member{}, "", ErrUnauthenticated
	}
	if err != nil {
		return Project{}, Member{}, "", err
	}
	if consumed.Valid {
		return Project{}, Member{}, "", ErrInviteConsumed
	}
	expiresAt, err := parseTime(expires)
	if err != nil {
		return Project{}, Member{}, "", err
	}
	now := s.now()
	if !expiresAt.After(now) {
		return Project{}, Member{}, "", ErrInviteExpired
	}
	member := Member{SchemaVersion: 1, ID: newID("mem"), ProjectID: projectID, Name: strings.TrimSpace(memberName), CreatedAt: now}
	credentialID, credentialToken, credentialHash, err := newSecret("cred")
	if err != nil {
		return Project{}, Member{}, "", err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO members(id,project_id,name,is_owner,created_at) VALUES(?,?,?,?,?)`, member.ID, member.ProjectID, member.Name, 0, formatTime(now)); err != nil {
		return Project{}, Member{}, "", mapConflict(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO credentials(id,member_id,token_hash,created_at) VALUES(?,?,?,?)`, credentialID, member.ID, credentialHash, formatTime(now)); err != nil {
		return Project{}, Member{}, "", err
	}
	result, err := tx.ExecContext(ctx, `UPDATE invites SET consumed_at=? WHERE id=? AND consumed_at IS NULL`, formatTime(now), inviteID)
	if err != nil {
		return Project{}, Member{}, "", err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return Project{}, Member{}, "", ErrInviteConsumed
	}
	project, err := getProjectTx(ctx, tx, projectID)
	if err != nil {
		return Project{}, Member{}, "", err
	}
	if err = tx.Commit(); err != nil {
		return Project{}, Member{}, "", err
	}
	return project, member, credentialToken, nil
}

func (s *Store) ListMembers(ctx context.Context, principal Principal) ([]Member, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,project_id,name,is_owner,created_at FROM members WHERE project_id=? ORDER BY name`, principal.Project.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Member
	for rows.Next() {
		var m Member
		var owner int
		var created string
		if err := rows.Scan(&m.ID, &m.ProjectID, &m.Name, &owner, &created); err != nil {
			return nil, err
		}
		m.SchemaVersion = 1
		m.Owner = owner == 1
		m.CreatedAt, _ = parseTime(created)
		result = append(result, m)
	}
	return result, rows.Err()
}

func (s *Store) PromoteMember(ctx context.Context, principal Principal, memberID string) (Member, error) {
	if !principal.Member.Owner {
		return Member{}, ErrForbidden
	}
	result, err := s.db.ExecContext(ctx, `UPDATE members SET is_owner=1 WHERE id=? AND project_id=?`, memberID, principal.Project.ID)
	if err != nil {
		return Member{}, err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return Member{}, ErrNotFound
	}
	return s.getMember(ctx, principal.Project.ID, memberID)
}

func (s *Store) RemoveMember(ctx context.Context, principal Principal, memberID string) error {
	if !principal.Member.Owner {
		return ErrForbidden
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var owner int
	if err = tx.QueryRowContext(ctx, `SELECT is_owner FROM members WHERE id=? AND project_id=?`, memberID, principal.Project.ID).Scan(&owner); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	if owner == 1 {
		var count int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM members WHERE project_id=? AND is_owner=1`, principal.Project.ID).Scan(&count); err != nil {
			return err
		}
		if count <= 1 {
			return ErrLastOwner
		}
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM members WHERE id=?`, memberID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) RotateCredential(ctx context.Context, principal Principal) (Credential, string, error) {
	now := s.now()
	id, token, hash, err := newSecret("cred")
	if err != nil {
		return Credential{}, "", err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Credential{}, "", err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE credentials SET revoked_at=? WHERE id=? AND revoked_at IS NULL`, formatTime(now), principal.Credential.ID); err != nil {
		return Credential{}, "", err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO credentials(id,member_id,token_hash,created_at) VALUES(?,?,?,?)`, id, principal.Member.ID, hash, formatTime(now)); err != nil {
		return Credential{}, "", err
	}
	if err = tx.Commit(); err != nil {
		return Credential{}, "", err
	}
	return Credential{SchemaVersion: 1, ID: id, MemberID: principal.Member.ID, CreatedAt: now}, token, nil
}

func (s *Store) RevokeCredential(ctx context.Context, principal Principal, credentialID string) error {
	if credentialID == "" {
		credentialID = principal.Credential.ID
	}
	if credentialID != principal.Credential.ID && !principal.Member.Owner {
		return ErrForbidden
	}
	now := s.now()
	result, err := s.db.ExecContext(ctx, `UPDATE credentials SET revoked_at=? WHERE id=? AND member_id IN (SELECT id FROM members WHERE project_id=?) AND revoked_at IS NULL`, formatTime(now), credentialID, principal.Project.ID)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) CredentialMemberID(ctx context.Context, projectID, credentialID string) (string, error) {
	var memberID string
	err := s.db.QueryRowContext(ctx, `SELECT c.member_id FROM credentials c JOIN members m ON m.id=c.member_id WHERE c.id=? AND m.project_id=?`, credentialID, projectID).Scan(&memberID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return memberID, err
}

func (s *Store) RegisterAgent(ctx context.Context, principal Principal, manifest protocolv1.AgentManifest) (Agent, error) {
	if err := manifest.Validate(); err != nil {
		return Agent{}, err
	}
	now := s.now()
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return Agent{}, err
	}
	var existingID string
	err = s.db.QueryRowContext(ctx, `SELECT id FROM agents WHERE project_id=? AND name=?`, principal.Project.ID, manifest.Name).Scan(&existingID)
	if err == nil {
		result, err := s.db.ExecContext(ctx, `UPDATE agents SET manifest_json=?,updated_at=? WHERE id=? AND owner_member_id=?`, string(encoded), formatTime(now), existingID, principal.Member.ID)
		if err != nil {
			return Agent{}, err
		}
		if n, _ := result.RowsAffected(); n == 0 {
			return Agent{}, ErrConflict
		}
		return s.GetAgent(ctx, principal, existingID)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Agent{}, err
	}
	agent := Agent{SchemaVersion: 1, ID: newID("agt"), ProjectID: principal.Project.ID, OwnerMemberID: principal.Member.ID, Manifest: manifest, UpdatedAt: now}
	_, err = s.db.ExecContext(ctx, `INSERT INTO agents(id,project_id,owner_member_id,name,manifest_json,updated_at) VALUES(?,?,?,?,?,?)`, agent.ID, agent.ProjectID, agent.OwnerMemberID, manifest.Name, string(encoded), formatTime(now))
	if err != nil {
		return Agent{}, mapConflict(err)
	}
	return agent, nil
}

func (s *Store) ListAgents(ctx context.Context, principal Principal) ([]Agent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,project_id,owner_member_id,manifest_json,updated_at FROM agents WHERE project_id=? ORDER BY name`, principal.Project.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Agent
	for rows.Next() {
		a, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, a)
	}
	return result, rows.Err()
}

func (s *Store) GetAgent(ctx context.Context, principal Principal, idOrName string) (Agent, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,project_id,owner_member_id,manifest_json,updated_at FROM agents WHERE project_id=? AND (id=? OR name=?)`, principal.Project.ID, idOrName, idOrName)
	a, err := scanAgent(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Agent{}, ErrNotFound
	}
	return a, err
}

func (s *Store) SetACL(ctx context.Context, principal Principal, agentID, memberID string, modes []protocolv1.RequestMode, grant bool) (ACL, error) {
	agent, err := s.GetAgent(ctx, principal, agentID)
	if err != nil {
		return ACL{}, err
	}
	if agent.OwnerMemberID != principal.Member.ID && !principal.Member.Owner {
		return ACL{}, ErrForbidden
	}
	if _, err = s.getMember(ctx, principal.Project.ID, memberID); err != nil {
		return ACL{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ACL{}, err
	}
	defer tx.Rollback()
	now := formatTime(s.now())
	for _, mode := range modes {
		if err := mode.Validate(); err != nil {
			return ACL{}, err
		}
		if !manifestSupportsMode(agent.Manifest, mode) {
			return ACL{}, fmt.Errorf("agent does not advertise %s mode", mode)
		}
		if grant {
			_, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO agent_acl(agent_id,member_id,mode,created_at) VALUES(?,?,?,?)`, agent.ID, memberID, string(mode), now)
		} else {
			_, err = tx.ExecContext(ctx, `DELETE FROM agent_acl WHERE agent_id=? AND member_id=? AND mode=?`, agent.ID, memberID, string(mode))
		}
		if err != nil {
			return ACL{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return ACL{}, err
	}
	return s.GetACL(ctx, agent.ID, memberID)
}

func manifestSupportsMode(manifest protocolv1.AgentManifest, wanted protocolv1.RequestMode) bool {
	for _, mode := range manifest.Modes {
		if mode == wanted {
			return true
		}
	}
	return false
}

func (s *Store) GetACL(ctx context.Context, agentID, memberID string) (ACL, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT mode FROM agent_acl WHERE agent_id=? AND member_id=? ORDER BY mode`, agentID, memberID)
	if err != nil {
		return ACL{}, err
	}
	defer rows.Close()
	acl := ACL{SchemaVersion: 1, AgentID: agentID, MemberID: memberID, Modes: []protocolv1.RequestMode{}}
	for rows.Next() {
		var mode protocolv1.RequestMode
		if err := rows.Scan(&mode); err != nil {
			return ACL{}, err
		}
		acl.Modes = append(acl.Modes, mode)
	}
	return acl, rows.Err()
}

func (s *Store) HasAccess(ctx context.Context, principal Principal, agentID string, mode protocolv1.RequestMode) (bool, error) {
	if err := mode.Validate(); err != nil {
		return false, err
	}
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_acl a JOIN agents g ON g.id=a.agent_id WHERE a.agent_id=? AND a.member_id=? AND a.mode=? AND g.project_id=?`, agentID, principal.Member.ID, string(mode), principal.Project.ID).Scan(&count)
	return count > 0, err
}

// RecordRequest accepts the in-memory request so callers cannot accidentally
// build a second representation. Only Request.Metadata is passed to SQLite.
func (s *Store) RecordRequest(ctx context.Context, request protocolv1.Request, status protocolv1.RequestStatus, updatedAt time.Time) error {
	_, replayed, err := s.BeginRequest(ctx, request, status, updatedAt)
	if err != nil || !replayed {
		return err
	}
	_, err = s.UpdateRequestStatus(ctx, request.ID, status, updatedAt)
	return err
}

// BeginRequest inserts a new body-free audit row. A repeated ID with the same
// complete binding returns the existing status; a different binding is a
// replay attack and is rejected.
func (s *Store) BeginRequest(ctx context.Context, request protocolv1.Request, status protocolv1.RequestStatus, updatedAt time.Time) (protocolv1.RequestMetadata, bool, error) {
	if err := request.Validate(); err != nil {
		return protocolv1.RequestMetadata{}, false, err
	}
	metadata := request.Metadata(status, updatedAt)
	if err := metadata.Validate(); err != nil {
		return protocolv1.RequestMetadata{}, false, err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO request_metadata(request_id,project_id,requester_member_id,agent_id,mode,status,body_bytes,body_sha256,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, metadata.ID, metadata.ProjectID, metadata.RequesterID, metadata.AgentID, string(metadata.Mode), string(metadata.Status), metadata.BodyBytes, metadata.BodySHA256, formatTime(metadata.CreatedAt), formatTime(metadata.UpdatedAt))
	if err == nil {
		return metadata, false, nil
	}
	if !strings.Contains(strings.ToLower(err.Error()), "unique") {
		return protocolv1.RequestMetadata{}, false, err
	}
	existing, getErr := s.GetRequestMetadata(ctx, request.ID)
	if getErr != nil {
		return protocolv1.RequestMetadata{}, false, getErr
	}
	if existing.ProjectID != metadata.ProjectID || existing.RequesterID != metadata.RequesterID || existing.AgentID != metadata.AgentID || existing.Mode != metadata.Mode || existing.BodyBytes != metadata.BodyBytes || existing.BodySHA256 != metadata.BodySHA256 {
		return protocolv1.RequestMetadata{}, false, ErrReplayMismatch
	}
	return existing, true, nil
}

func (s *Store) UpdateRequestStatus(ctx context.Context, requestID string, status protocolv1.RequestStatus, updatedAt time.Time) (protocolv1.RequestMetadata, error) {
	if err := status.Validate(); err != nil {
		return protocolv1.RequestMetadata{}, err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE request_metadata SET status=?,updated_at=? WHERE request_id=?`, string(status), formatTime(updatedAt), requestID)
	if err != nil {
		return protocolv1.RequestMetadata{}, err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return protocolv1.RequestMetadata{}, ErrNotFound
	}
	return s.GetRequestMetadata(ctx, requestID)
}

func (s *Store) GetRequestMetadata(ctx context.Context, id string) (protocolv1.RequestMetadata, error) {
	var m protocolv1.RequestMetadata
	var mode, status, created, updated string
	err := s.db.QueryRowContext(ctx, `SELECT request_id,project_id,requester_member_id,agent_id,mode,status,body_bytes,body_sha256,created_at,updated_at FROM request_metadata WHERE request_id=?`, id).Scan(&m.ID, &m.ProjectID, &m.RequesterID, &m.AgentID, &mode, &status, &m.BodyBytes, &m.BodySHA256, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return m, ErrNotFound
	}
	if err != nil {
		return m, err
	}
	m.SchemaVersion = 1
	m.Mode = protocolv1.RequestMode(mode)
	m.Status = protocolv1.RequestStatus(status)
	m.CreatedAt, _ = parseTime(created)
	m.UpdatedAt, _ = parseTime(updated)
	return m, nil
}

func (s *Store) getMember(ctx context.Context, projectID, memberID string) (Member, error) {
	var m Member
	var owner int
	var created string
	err := s.db.QueryRowContext(ctx, `SELECT id,project_id,name,is_owner,created_at FROM members WHERE id=? AND project_id=?`, memberID, projectID).Scan(&m.ID, &m.ProjectID, &m.Name, &owner, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return m, ErrNotFound
	}
	if err != nil {
		return m, err
	}
	m.SchemaVersion = 1
	m.Owner = owner == 1
	m.CreatedAt, _ = parseTime(created)
	return m, nil
}

type scanner interface{ Scan(...any) error }

func scanAgent(row scanner) (Agent, error) {
	var a Agent
	var manifest, updated string
	err := row.Scan(&a.ID, &a.ProjectID, &a.OwnerMemberID, &manifest, &updated)
	if err != nil {
		return a, err
	}
	a.SchemaVersion = 1
	if err = json.Unmarshal([]byte(manifest), &a.Manifest); err != nil {
		return a, err
	}
	a.UpdatedAt, _ = parseTime(updated)
	return a, nil
}
func getProjectTx(ctx context.Context, tx *sql.Tx, id string) (Project, error) {
	var p Project
	var created string
	err := tx.QueryRowContext(ctx, `SELECT id,name,created_at FROM projects WHERE id=?`, id).Scan(&p.ID, &p.Name, &created)
	if err != nil {
		return p, err
	}
	p.SchemaVersion = 1
	p.CreatedAt, _ = parseTime(created)
	return p, nil
}
func formatTime(t time.Time) string             { return t.UTC().Format(time.RFC3339Nano) }
func parseTime(value string) (time.Time, error) { return time.Parse(time.RFC3339Nano, value) }
func newID(prefix string) string {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		panic(err)
	}
	return prefix + "_" + base64.RawURLEncoding.EncodeToString(raw[:])
}
func newSecret(prefix string) (id, token, hash string, err error) {
	var raw [32]byte
	if _, err = rand.Read(raw[:]); err != nil {
		return "", "", "", err
	}
	token = prefix + "_" + base64.RawURLEncoding.EncodeToString(raw[:])
	return newID(prefix), token, secretHash(token), nil
}
func secretHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
func mapConflict(err error) error {
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "unique") {
		return fmt.Errorf("%w: %v", ErrConflict, err)
	}
	return err
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

package lanhost

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	protocolv2 "github.com/wWzZb/peercontext/pkg/protocol/v2"
)

var (
	ErrNotFound        = errors.New("not found")
	ErrInviteInvalid   = errors.New("invitation is invalid")
	ErrInviteConsumed  = errors.New("invitation has already been used")
	ErrInviteExpired   = errors.New("invitation has expired")
	ErrMemberForbidden = errors.New("member is not allowed")
)

type Store struct {
	db *sql.DB
}

func OpenStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	const schema = `
CREATE TABLE IF NOT EXISTS projects (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  host_member_id TEXT NOT NULL,
  host_public_key TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS members (
  project_id TEXT NOT NULL,
  id TEXT NOT NULL,
  name TEXT NOT NULL,
  public_key TEXT NOT NULL,
  created_at TEXT NOT NULL,
  PRIMARY KEY(project_id, id),
  UNIQUE(project_id, public_key)
);
CREATE TABLE IF NOT EXISTS invitations (
  project_id TEXT NOT NULL,
  id TEXT NOT NULL,
  public_key TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  consumed_at TEXT,
  PRIMARY KEY(project_id, id)
);
CREATE TABLE IF NOT EXISTS agents (
  project_id TEXT NOT NULL,
  id TEXT NOT NULL,
  owner_member_id TEXT NOT NULL,
  name TEXT NOT NULL,
  manifest_json BLOB NOT NULL,
  online INTEGER NOT NULL DEFAULT 0,
  last_seen_at TEXT,
  created_at TEXT NOT NULL,
  PRIMARY KEY(project_id, id),
  UNIQUE(project_id, name)
);
CREATE TABLE IF NOT EXISTS requests (
  project_id TEXT NOT NULL,
  id TEXT NOT NULL,
  requester_member_id TEXT NOT NULL,
  agent_id TEXT NOT NULL,
  status TEXT NOT NULL,
	  body_bytes INTEGER NOT NULL,
	  body_sha256 TEXT NOT NULL,
  created_at TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  started_at TEXT,
  completed_at TEXT,
  PRIMARY KEY(project_id, id)
);
CREATE INDEX IF NOT EXISTS idx_agents_project ON agents(project_id);
CREATE INDEX IF NOT EXISTS idx_requests_project ON requests(project_id);
`
	_, err := s.db.ExecContext(ctx, schema)
	return err
}

func (s *Store) CreateProject(ctx context.Context, project protocolv2.Project, owner protocolv2.Member) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO projects(id,name,host_member_id,host_public_key,created_at) VALUES(?,?,?,?,?)`, project.ID, project.Name, owner.ID, base64.RawURLEncoding.EncodeToString(project.HostPublicKey), project.CreatedAt.UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO members(project_id,id,name,public_key,created_at) VALUES(?,?,?,?,?)`, owner.ProjectID, owner.ID, owner.Name, base64.RawURLEncoding.EncodeToString(owner.PublicKey), owner.CreatedAt.UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Project(ctx context.Context, projectID string) (protocolv2.Project, error) {
	var p protocolv2.Project
	var hostPublicKey, created string
	err := s.db.QueryRowContext(ctx, `SELECT id,name,host_public_key,created_at FROM projects WHERE id=?`, projectID).Scan(&p.ID, &p.Name, &hostPublicKey, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	if err != nil {
		return p, err
	}
	p.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	p.SchemaVersion = protocolv2.SchemaVersion
	p.HostPublicKey, _ = base64.RawURLEncoding.DecodeString(hostPublicKey)
	return p, nil
}

func (s *Store) Projects(ctx context.Context) ([]protocolv2.Project, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,host_public_key,created_at FROM projects ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []protocolv2.Project
	for rows.Next() {
		var p protocolv2.Project
		var hostPublicKey, created string
		if err := rows.Scan(&p.ID, &p.Name, &hostPublicKey, &created); err != nil {
			return nil, err
		}
		p.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		p.SchemaVersion = protocolv2.SchemaVersion
		p.HostPublicKey, _ = base64.RawURLEncoding.DecodeString(hostPublicKey)
		result = append(result, p)
	}
	return result, rows.Err()
}

func (s *Store) DeleteProject(ctx context.Context, projectID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, table := range []string{"requests", "agents", "invitations", "members"} {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE project_id=?", projectID); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM projects WHERE id=?`, projectID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) AddInvitation(ctx context.Context, projectID, invitationID string, publicKey ed25519.PublicKey, expiresAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO invitations(project_id,id,public_key,expires_at) VALUES(?,?,?,?)`, projectID, invitationID, base64.RawURLEncoding.EncodeToString(publicKey), expiresAt.UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) ConsumeInvitation(ctx context.Context, request protocolv2.JoinRequest, now time.Time) (protocolv2.Project, error) {
	p, err := s.Project(ctx, request.ProjectID)
	if err != nil {
		return p, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return p, err
	}
	defer tx.Rollback()
	var publicKeyText, expiresText string
	var consumed sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT public_key,expires_at,consumed_at FROM invitations WHERE project_id=? AND id=?`, request.ProjectID, request.InviteID).Scan(&publicKeyText, &expiresText, &consumed)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrInviteInvalid
	}
	if err != nil {
		return p, err
	}
	if consumed.Valid {
		return p, ErrInviteConsumed
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, expiresText)
	if err != nil || !now.Before(expiresAt) {
		return p, ErrInviteExpired
	}
	publicKey, err := base64.RawURLEncoding.DecodeString(publicKeyText)
	if err != nil || len(publicKey) != ed25519.PublicKeySize || request.Verify(ed25519.PublicKey(publicKey), now, protocolv2.DefaultSignatureMaxAge) != nil {
		return p, ErrInviteInvalid
	}
	if len(request.MemberPublicKey) != ed25519.PublicKeySize {
		return p, ErrInviteInvalid
	}
	memberID := memberIDFromPublicKey(request.MemberPublicKey)
	member := protocolv2.Member{SchemaVersion: protocolv2.SchemaVersion, ID: memberID, ProjectID: request.ProjectID, Name: request.MemberName, PublicKey: request.MemberPublicKey, CreatedAt: now.UTC()}
	if _, err := tx.ExecContext(ctx, `INSERT INTO members(project_id,id,name,public_key,created_at) VALUES(?,?,?,?,?)`, member.ProjectID, member.ID, member.Name, base64.RawURLEncoding.EncodeToString(request.MemberPublicKey), member.CreatedAt.Format(time.RFC3339Nano)); err != nil {
		return p, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE invitations SET consumed_at=? WHERE project_id=? AND id=? AND consumed_at IS NULL`, now.UTC().Format(time.RFC3339Nano), request.ProjectID, request.InviteID)
	if err != nil {
		return p, err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return p, ErrInviteConsumed
	}
	return p, tx.Commit()
}

func (s *Store) Member(ctx context.Context, projectID, memberID string) (protocolv2.Member, error) {
	var m protocolv2.Member
	var created string
	var publicKey string
	err := s.db.QueryRowContext(ctx, `SELECT id,project_id,name,public_key,created_at FROM members WHERE project_id=? AND id=?`, projectID, memberID).Scan(&m.ID, &m.ProjectID, &m.Name, &publicKey, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return m, ErrMemberForbidden
	}
	if err != nil {
		return m, err
	}
	m.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	m.SchemaVersion = protocolv2.SchemaVersion
	m.PublicKey, _ = base64.RawURLEncoding.DecodeString(publicKey)
	p, projectErr := s.Project(ctx, projectID)
	if projectErr == nil {
		m.Owner = string(m.PublicKey) == string(p.HostPublicKey)
	}
	return m, nil
}

func (s *Store) Members(ctx context.Context, projectID string) ([]protocolv2.Member, error) {
	project, err := s.Project(ctx, projectID)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,project_id,name,public_key,created_at FROM members WHERE project_id=? ORDER BY created_at`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []protocolv2.Member
	for rows.Next() {
		var m protocolv2.Member
		var publicKey, created string
		if err := rows.Scan(&m.ID, &m.ProjectID, &m.Name, &publicKey, &created); err != nil {
			return nil, err
		}
		m.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		m.SchemaVersion = protocolv2.SchemaVersion
		m.PublicKey, _ = base64.RawURLEncoding.DecodeString(publicKey)
		m.Owner = string(m.PublicKey) == string(project.HostPublicKey)
		result = append(result, m)
	}
	return result, rows.Err()
}

func (s *Store) RemoveMember(ctx context.Context, projectID, actorID, memberID string) error {
	_, err := s.Project(ctx, projectID)
	if err != nil {
		return err
	}
	owner, err := s.Member(ctx, projectID, actorID)
	if err != nil || !owner.Owner {
		return ErrMemberForbidden
	}
	target, err := s.Member(ctx, projectID, memberID)
	if err != nil || target.Owner {
		return ErrMemberForbidden
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM agents WHERE project_id=? AND owner_member_id=?`, projectID, memberID); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM members WHERE project_id=? AND id=?`, projectID, memberID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

func (s *Store) RegisterAgent(ctx context.Context, projectID, memberID, agentID string, manifest protocolv2.AgentManifest, now time.Time) (protocolv2.Agent, error) {
	if _, err := s.Member(ctx, projectID, memberID); err != nil {
		return protocolv2.Agent{}, err
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return protocolv2.Agent{}, err
	}
	agent := protocolv2.Agent{SchemaVersion: protocolv2.SchemaVersion, ID: agentID, ProjectID: projectID, OwnerMemberID: memberID, Manifest: manifest, UpdatedAt: now.UTC()}
	result, err := s.db.ExecContext(ctx, `INSERT INTO agents(project_id,id,owner_member_id,name,manifest_json,online,created_at) VALUES(?,?,?,?,?,0,?)
	ON CONFLICT(project_id,id) DO UPDATE SET name=excluded.name,manifest_json=excluded.manifest_json WHERE agents.owner_member_id=excluded.owner_member_id`, projectID, agent.ID, memberID, manifest.Name, encoded, agent.UpdatedAt.Format(time.RFC3339Nano))
	if err == nil {
		if affected, _ := result.RowsAffected(); affected == 0 {
			err = ErrMemberForbidden
		}
	}
	return agent, err
}

func (s *Store) SetAgentOnline(ctx context.Context, projectID, agentID, memberID string, online bool, now time.Time) error {
	value := 0
	if online {
		value = 1
	}
	res, err := s.db.ExecContext(ctx, `UPDATE agents SET online=?,last_seen_at=? WHERE project_id=? AND id=? AND owner_member_id=?`, value, now.UTC().Format(time.RFC3339Nano), projectID, agentID, memberID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) Agents(ctx context.Context, projectID string) ([]protocolv2.Agent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,project_id,owner_member_id,name,manifest_json,online,last_seen_at,created_at FROM agents WHERE project_id=? ORDER BY name`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []protocolv2.Agent
	for rows.Next() {
		agent, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, agent)
	}
	return result, rows.Err()
}

func (s *Store) Agent(ctx context.Context, projectID, selector string) (protocolv2.Agent, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,project_id,owner_member_id,name,manifest_json,online,last_seen_at,created_at FROM agents WHERE project_id=? AND (id=? OR name=?)`, projectID, selector, selector)
	agent, err := scanAgent(row)
	if errors.Is(err, sql.ErrNoRows) {
		return agent, ErrNotFound
	}
	return agent, err
}

func (s *Store) RemoveAgent(ctx context.Context, projectID, actorID, selector string) error {
	agent, err := s.Agent(ctx, projectID, selector)
	if err != nil {
		return err
	}
	actor, err := s.Member(ctx, projectID, actorID)
	if err != nil {
		return err
	}
	if actorID != agent.OwnerMemberID && !actor.Owner {
		return ErrMemberForbidden
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM agents WHERE project_id=? AND id=?`, projectID, agent.ID)
	return err
}

type scanner interface{ Scan(...any) error }

func scanAgent(row scanner) (protocolv2.Agent, error) {
	var agent protocolv2.Agent
	var name string
	var manifest []byte
	var online int
	var lastSeen sql.NullString
	var created string
	if err := row.Scan(&agent.ID, &agent.ProjectID, &agent.OwnerMemberID, &name, &manifest, &online, &lastSeen, &created); err != nil {
		return agent, err
	}
	if err := json.Unmarshal(manifest, &agent.Manifest); err != nil {
		return agent, fmt.Errorf("decode agent manifest: %w", err)
	}
	agent.Online = online == 1
	agent.SchemaVersion = protocolv2.SchemaVersion
	agent.UpdatedAt, _ = time.Parse(time.RFC3339Nano, created)
	if lastSeen.Valid {
		agent.UpdatedAt, _ = time.Parse(time.RFC3339Nano, lastSeen.String)
	}
	return agent, nil
}

func memberIDFromPublicKey(publicKey []byte) string {
	sum := sha256.Sum256(publicKey)
	return "mem_" + base64.RawURLEncoding.EncodeToString(sum[:9])
}

func (s *Store) CreateRequest(ctx context.Context, request protocolv2.Request) error {
	expiresAt := request.CreatedAt.Add(protocolv2.DefaultRequestTimeout)
	_, err := s.db.ExecContext(ctx, `INSERT INTO requests(project_id,id,requester_member_id,agent_id,status,body_bytes,body_sha256,created_at,expires_at) VALUES(?,?,?,?,?,?,?,?,?)`, request.ProjectID, request.ID, request.RequesterID, request.AgentID, protocolv2.StatusRunning, len(request.Body), request.BodySHA256, request.CreatedAt.UTC().Format(time.RFC3339Nano), expiresAt.UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) UpdateRequest(ctx context.Context, projectID, requestID string, status protocolv2.RequestStatus, at time.Time) error {
	var column string
	switch status {
	case protocolv2.StatusRunning:
		column = "started_at"
	case protocolv2.StatusSucceeded, protocolv2.StatusFailed, protocolv2.StatusCanceled, protocolv2.StatusExpired:
		column = "completed_at"
	default:
		return fmt.Errorf("unsupported request status %q", status)
	}
	query := fmt.Sprintf(`UPDATE requests SET status=?,%s=? WHERE project_id=? AND id=?`, column)
	_, err := s.db.ExecContext(ctx, query, status, at.UTC().Format(time.RFC3339Nano), projectID, requestID)
	return err
}

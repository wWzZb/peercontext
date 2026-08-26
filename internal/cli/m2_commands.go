package cli

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/wWzZb/peercontext/internal/codex"
	"github.com/wWzZb/peercontext/internal/doctor"
	"github.com/wWzZb/peercontext/internal/localstate"
	"github.com/wWzZb/peercontext/internal/relay"
	"github.com/wWzZb/peercontext/internal/relayclient"
	"github.com/wWzZb/peercontext/internal/skillbundle"
	"github.com/wWzZb/peercontext/internal/version"
	"github.com/wWzZb/peercontext/internal/worktree"
	"github.com/wWzZb/peercontext/pkg/clioutput"
	protocolv1 "github.com/wWzZb/peercontext/pkg/protocol/v1"
)

func runM2(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) (bool, clioutput.ExitCode) {
	if (args[0] == "ask" || args[0] == "task") && os.Getenv("PEERCTX_INBOUND_REQUEST") != "" {
		return true, writeCLIError(stderr, &commandError{exit: clioutput.ExitAuthorization, errorType: "authorization", subtype: "recursive_request", code: "recursive_request_blocked", message: "Inbound Codex requests cannot create another PeerContext request.", hint: "Return the result to the original requester without calling peerctx ask or task."})
	}
	switch args[0] {
	case "relay":
		return true, runRelay(ctx, args[1:], stdout, stderr)
	case "project":
		return true, runProject(ctx, args[1:], stdout, stderr)
	case "credential":
		return true, runCredential(ctx, args[1:], stdout, stderr)
	case "agent":
		return true, runAgent(ctx, args[1:], stdout, stderr)
	case "ask":
		return true, runAsk(ctx, args[1:], stdin, stdout, stderr)
	case "task":
		return true, runTask(ctx, args[1:], stdin, stdout, stderr)
	case "request":
		return true, runRequest(ctx, args[1:], stdout, stderr)
	case "worktree":
		return true, runWorktree(args[1:], stdout, stderr)
	case "skills":
		return true, runSkills(args[1:], stdout, stderr)
	case "doctor":
		return true, runDoctor(ctx, args[1:], stdout, stderr)
	default:
		return false, 0
	}
}

func runRelay(ctx context.Context, args []string, stdout, stderr io.Writer) clioutput.ExitCode {
	if len(args) == 0 || args[0] != "serve" {
		return writeCLIError(stderr, usageError("relay_command", "Use: peerctx relay serve [flags]."))
	}
	flags := flag.NewFlagSet("relay serve", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	listen := flags.String("listen", "127.0.0.1:7777", "")
	database := flags.String("database", defaultRelayDatabase(), "")
	cert := flags.String("tls-cert", "", "")
	key := flags.String("tls-key", "", "")
	logFile := flags.String("log-file", "", "")
	if err := flags.Parse(args[1:]); err != nil {
		return writeCLIError(stderr, usageError("relay_flags", "Use: peerctx relay serve [--listen HOST:PORT] [--database PATH] [--tls-cert PATH --tls-key PATH] [--log-file PATH]."))
	}
	if err := relay.ValidateTLSRequirement(*listen, *cert, *key); err != nil {
		return writeMappedError(stderr, err)
	}
	if err := os.MkdirAll(filepath.Dir(*database), 0700); err != nil {
		return writeMappedError(stderr, err)
	}
	if *logFile == "" {
		*logFile = *database + ".log"
	}
	if err := os.MkdirAll(filepath.Dir(*logFile), 0700); err != nil {
		return writeMappedError(stderr, err)
	}
	logWriter, err := os.OpenFile(*logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return writeMappedError(stderr, err)
	}
	defer logWriter.Close()
	logger := slog.New(slog.NewJSONHandler(logWriter, nil))
	store, err := relay.OpenStore(*database, logger)
	if err != nil {
		return writeMappedError(stderr, err)
	}
	defer store.Close()
	server, err := relay.NewServer(store, logger)
	if err != nil {
		return writeMappedError(stderr, err)
	}
	signalCtx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := relay.Serve(signalCtx, *listen, *cert, *key, server.Handler(), func(address net.Addr) error {
		if code := writeSuccess(stdout, map[string]any{"schema_version": 1, "listen": address.String(), "database": *database, "tls": *cert != ""}, ""); code != clioutput.ExitOK {
			return errors.New("write Relay readiness output")
		}
		return nil
	}); err != nil {
		return clioutput.ExitConnection
	}
	return clioutput.ExitOK
}

func runProject(ctx context.Context, args []string, stdout, stderr io.Writer) clioutput.ExitCode {
	if len(args) == 0 {
		return writeCLIError(stderr, usageError("project_command", "Use project create, list, use, join, invite, or member."))
	}
	switch args[0] {
	case "create":
		return projectCreate(ctx, args[1:], stdout, stderr)
	case "join":
		return projectJoin(ctx, args[1:], stdout, stderr)
	case "list":
		return projectList(stdout, stderr)
	case "use":
		return projectUse(args[1:], stdout, stderr)
	case "invite":
		if len(args) > 1 && args[1] == "create" {
			return projectInvite(ctx, args[2:], stdout, stderr)
		}
	case "member":
		return projectMember(ctx, args[1:], stdout, stderr)
	}
	return writeCLIError(stderr, usageError("project_command", "Unknown project command."))
}

func projectCreate(ctx context.Context, args []string, stdout, stderr io.Writer) clioutput.ExitCode {
	flags := newFlags("project create")
	relayURL := flags.String("relay", "http://127.0.0.1:7777", "")
	name := flags.String("name", "", "")
	owner := flags.String("owner", "", "")
	credentialFile := flags.String("credential-file", "", "")
	if err := flags.Parse(args); err != nil || *name == "" || *owner == "" {
		return writeCLIError(stderr, usageError("project_create_arguments", "Use: peerctx project create --name NAME --owner NAME [--relay URL] [--credential-file PATH]."))
	}
	manager, err := localstate.DefaultManager()
	if err != nil {
		return writeMappedError(stderr, err)
	}
	if err = manager.PrepareCredentialStore(*credentialFile); err != nil {
		return writeMappedError(stderr, err)
	}
	client, err := relayclient.New(*relayURL, "")
	if err != nil {
		return writeMappedError(stderr, err)
	}
	session, err := client.CreateProject(ctx, *name, *owner)
	if err != nil {
		return writeMappedError(stderr, err)
	}
	profile := localstate.Profile{ProjectID: session.Project.ID, ProjectName: session.Project.Name, RelayURL: *relayURL, MemberID: session.Member.ID, MemberName: session.Member.Name}
	if err = manager.PutProfile(profile, session.CredentialToken, *credentialFile); err != nil {
		return writeMappedError(stderr, err)
	}
	return writeSuccess(stdout, map[string]any{"schema_version": 1, "project": session.Project, "member": session.Member, "credential_store": credentialStoreName(*credentialFile)}, "")
}

func projectJoin(ctx context.Context, args []string, stdout, stderr io.Writer) clioutput.ExitCode {
	flags := newFlags("project join")
	relayURL := flags.String("relay", "http://127.0.0.1:7777", "")
	invite := flags.String("invite-token", "", "")
	member := flags.String("member", "", "")
	credentialFile := flags.String("credential-file", "", "")
	if err := flags.Parse(args); err != nil || *invite == "" || *member == "" {
		return writeCLIError(stderr, usageError("project_join_arguments", "Use: peerctx project join --invite-token TOKEN --member NAME [--relay URL] [--credential-file PATH]."))
	}
	manager, err := localstate.DefaultManager()
	if err != nil {
		return writeMappedError(stderr, err)
	}
	if err = manager.PrepareCredentialStore(*credentialFile); err != nil {
		return writeMappedError(stderr, err)
	}
	client, err := relayclient.New(*relayURL, "")
	if err != nil {
		return writeMappedError(stderr, err)
	}
	session, err := client.JoinProject(ctx, *invite, *member)
	if err != nil {
		return writeMappedError(stderr, err)
	}
	profile := localstate.Profile{ProjectID: session.Project.ID, ProjectName: session.Project.Name, RelayURL: *relayURL, MemberID: session.Member.ID, MemberName: session.Member.Name}
	if err = manager.PutProfile(profile, session.CredentialToken, *credentialFile); err != nil {
		return writeMappedError(stderr, err)
	}
	return writeSuccess(stdout, map[string]any{"schema_version": 1, "project": session.Project, "member": session.Member, "credential_store": credentialStoreName(*credentialFile)}, "")
}

func projectList(stdout, stderr io.Writer) clioutput.ExitCode {
	manager, err := localstate.DefaultManager()
	if err != nil {
		return writeMappedError(stderr, err)
	}
	state, profiles, err := manager.Profiles()
	if err != nil {
		return writeMappedError(stderr, err)
	}
	type item struct {
		SchemaVersion int    `json:"schema_version"`
		ProjectID     string `json:"project_id"`
		ProjectName   string `json:"project_name"`
		RelayURL      string `json:"relay_url"`
		MemberID      string `json:"member_id"`
		MemberName    string `json:"member_name"`
		Current       bool   `json:"current"`
	}
	items := make([]item, 0, len(profiles))
	for _, profile := range profiles {
		items = append(items, item{1, profile.ProjectID, profile.ProjectName, profile.RelayURL, profile.MemberID, profile.MemberName, state.CurrentProjectID == profile.ProjectID})
	}
	return writeSuccess(stdout, map[string]any{"schema_version": 1, "projects": items}, "")
}
func projectUse(args []string, stdout, stderr io.Writer) clioutput.ExitCode {
	if len(args) != 1 {
		return writeCLIError(stderr, usageError("project_use_arguments", "Use: peerctx project use PROJECT_ID."))
	}
	manager, err := localstate.DefaultManager()
	if err == nil {
		err = manager.Use(args[0])
	}
	if err != nil {
		return writeMappedError(stderr, err)
	}
	return writeSuccess(stdout, map[string]any{"schema_version": 1, "project_id": args[0], "current": true}, "")
}

func projectInvite(ctx context.Context, args []string, stdout, stderr io.Writer) clioutput.ExitCode {
	flags := newFlags("project invite create")
	expires := flags.Duration("expires-in", 10*time.Minute, "")
	if err := flags.Parse(args); err != nil || *expires <= 0 {
		return writeCLIError(stderr, usageError("invite_arguments", "Use: peerctx project invite create [--expires-in 10m]."))
	}
	_, _, client, err := currentClient()
	if err != nil {
		return writeMappedError(stderr, err)
	}
	result, err := client.CreateInvite(ctx, *expires)
	if err != nil {
		return writeMappedError(stderr, err)
	}
	return writeSuccess(stdout, result, "")
}

func projectMember(ctx context.Context, args []string, stdout, stderr io.Writer) clioutput.ExitCode {
	if len(args) == 0 {
		return writeCLIError(stderr, usageError("member_command", "Use project member list, promote, or remove."))
	}
	_, _, client, err := currentClient()
	if err != nil {
		return writeMappedError(stderr, err)
	}
	switch args[0] {
	case "list":
		if len(args) != 1 {
			return writeCLIError(stderr, usageError("member_list_arguments", "Use: peerctx project member list."))
		}
		members, err := client.ListMembers(ctx)
		if err != nil {
			return writeMappedError(stderr, err)
		}
		return writeSuccess(stdout, map[string]any{"schema_version": 1, "members": members}, "")
	case "promote":
		if len(args) != 2 {
			return writeCLIError(stderr, usageError("member_promote_arguments", "Use: peerctx project member promote MEMBER_ID."))
		}
		member, err := client.PromoteMember(ctx, args[1])
		if err != nil {
			return writeMappedError(stderr, err)
		}
		return writeSuccess(stdout, member, "")
	case "remove":
		if len(args) != 2 {
			return writeCLIError(stderr, usageError("member_remove_arguments", "Use: peerctx project member remove MEMBER_ID."))
		}
		if err := client.RemoveMember(ctx, args[1]); err != nil {
			return writeMappedError(stderr, err)
		}
		return writeSuccess(stdout, map[string]any{"schema_version": 1, "member_id": args[1], "removed": true}, "")
	default:
		return writeCLIError(stderr, usageError("member_command", "Unknown project member command."))
	}
}

func runCredential(ctx context.Context, args []string, stdout, stderr io.Writer) clioutput.ExitCode {
	if len(args) == 0 {
		return writeCLIError(stderr, usageError("credential_command", "Use credential status, rotate, or revoke."))
	}
	manager, profile, client, err := currentClient()
	if err != nil {
		return writeMappedError(stderr, err)
	}
	switch args[0] {
	case "status":
		if len(args) != 1 {
			return writeCLIError(stderr, usageError("credential_status_arguments", "Use: peerctx credential status."))
		}
		status, err := client.CredentialStatus(ctx)
		if err != nil {
			return writeMappedError(stderr, err)
		}
		return writeSuccess(stdout, status, "")
	case "rotate":
		if len(args) != 1 {
			return writeCLIError(stderr, usageError("credential_rotate_arguments", "Use: peerctx credential rotate."))
		}
		result, err := client.RotateCredential(ctx)
		if err != nil {
			return writeMappedError(stderr, err)
		}
		if err = manager.ReplaceToken(profile, result.CredentialToken); err != nil {
			return writeMappedError(stderr, err)
		}
		return writeSuccess(stdout, map[string]any{"schema_version": 1, "credential": result.Credential, "rotated": true}, "")
	case "revoke":
		flags := newFlags("credential revoke")
		id := flags.String("credential", "", "")
		if err := flags.Parse(args[1:]); err != nil {
			return writeCLIError(stderr, usageError("credential_revoke_arguments", "Use: peerctx credential revoke [--credential ID]."))
		}
		if err := client.RevokeCredential(ctx, *id); err != nil {
			return writeMappedError(stderr, err)
		}
		if *id == "" {
			_ = manager.DeleteToken(profile)
		}
		return writeSuccess(stdout, map[string]any{"schema_version": 1, "credential_id": *id, "revoked": true}, "")
	default:
		return writeCLIError(stderr, usageError("credential_command", "Unknown credential command."))
	}
}

func runAgent(ctx context.Context, args []string, stdout, stderr io.Writer) clioutput.ExitCode {
	if len(args) == 0 {
		return writeCLIError(stderr, usageError("agent_command", "Use agent register, list, get, serve, or access."))
	}
	switch args[0] {
	case "register":
		return agentRegister(ctx, args[1:], stdout, stderr)
	case "list":
		return agentList(ctx, args[1:], stdout, stderr)
	case "get":
		return agentGet(ctx, args[1:], stdout, stderr)
	case "serve":
		return agentServe(ctx, args[1:], stdout, stderr)
	case "access":
		return agentAccess(ctx, args[1:], stdout, stderr)
	default:
		return writeCLIError(stderr, usageError("agent_command", "Unknown agent command."))
	}
}

func agentRegister(ctx context.Context, args []string, stdout, stderr io.Writer) clioutput.ExitCode {
	flags := newFlags("agent register")
	repository := flags.String("repository", "", "")
	name := flags.String("name", "", "")
	summary := flags.String("summary", "", "")
	tags := flags.String("tags", "", "")
	capabilities := flags.String("capabilities", "", "")
	modesText := flags.String("modes", "read", "")
	if err := flags.Parse(args); err != nil || *repository == "" || *name == "" || *summary == "" {
		return writeCLIError(stderr, usageError("agent_register_arguments", "Use: peerctx agent register --repository PATH --name NAME --summary TEXT [--tags CSV] [--capabilities CSV] [--modes read,write]."))
	}
	modes, err := parseModes(*modesText)
	if err != nil {
		return writeMappedError(stderr, err)
	}
	manifest := protocolv1.AgentManifest{SchemaVersion: 1, Name: *name, Summary: *summary, Tags: splitCSV(*tags), Capabilities: splitCSV(*capabilities), Modes: modes}
	manager, profile, client, err := currentClient()
	if err != nil {
		return writeMappedError(stderr, err)
	}
	agent, err := client.RegisterAgent(ctx, manifest)
	if err != nil {
		return writeMappedError(stderr, err)
	}
	if err = manager.PutAgent(localstate.LocalAgent{AgentID: agent.ID, ProjectID: profile.ProjectID, Repository: *repository}); err != nil {
		return writeMappedError(stderr, err)
	}
	return writeSuccess(stdout, agent, "")
}
func agentList(ctx context.Context, args []string, stdout, stderr io.Writer) clioutput.ExitCode {
	if len(args) != 0 {
		return writeCLIError(stderr, usageError("agent_list_arguments", "Use: peerctx agent list."))
	}
	_, _, client, err := currentClient()
	if err != nil {
		return writeMappedError(stderr, err)
	}
	agents, err := client.ListAgents(ctx)
	if err != nil {
		return writeMappedError(stderr, err)
	}
	return writeSuccess(stdout, map[string]any{"schema_version": 1, "agents": agents}, "")
}
func agentGet(ctx context.Context, args []string, stdout, stderr io.Writer) clioutput.ExitCode {
	if len(args) != 1 {
		return writeCLIError(stderr, usageError("agent_get_arguments", "Use: peerctx agent get AGENT."))
	}
	_, _, client, err := currentClient()
	if err != nil {
		return writeMappedError(stderr, err)
	}
	agent, err := client.GetAgent(ctx, args[0])
	if err != nil {
		return writeMappedError(stderr, err)
	}
	return writeSuccess(stdout, agent, "")
}

func agentServe(ctx context.Context, args []string, stdout, stderr io.Writer) clioutput.ExitCode {
	if len(args) != 1 {
		return writeCLIError(stderr, usageError("agent_serve_arguments", "Use: peerctx agent serve AGENT."))
	}
	manager, profile, client, err := currentClient()
	if err != nil {
		return writeMappedError(stderr, err)
	}
	agent, err := client.GetAgent(ctx, args[0])
	if err != nil {
		return writeMappedError(stderr, err)
	}
	localAgent, err := manager.Agent(profile.ProjectID, agent.ID)
	if err != nil {
		return writeMappedError(stderr, err)
	}
	runtimeAdapter, err := codex.NewIsolatedAdapter()
	if err != nil {
		return writeRuntimeError(stderr, err)
	}
	signalCtx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()
	wroteReady := false
	err = client.ServeAgentWithJobHandler(signalCtx, agent.ID, func(ready map[string]any) {
		if !wroteReady {
			_ = clioutput.WriteSuccess(stdout, ready, clioutput.Meta{Version: version.Current})
			wroteReady = true
		}
	}, func(requestCtx context.Context, request protocolv1.Request, baseCommit string) (protocolv1.Response, *protocolv1.RequestFailure) {
		invocation := codex.Invocation{Workspace: localAgent.Repository, Mode: request.Mode, Stdin: bytes.NewReader(request.Body)}
		var publicWorktree *protocolv1.WorktreeResult
		if request.Mode == protocolv1.ModeWrite {
			worktrees, managerErr := worktree.New(manager.Directory())
			if managerErr != nil {
				return protocolv1.Response{}, worktreeFailure(request.ID, managerErr)
			}
			record, createErr := worktrees.Create(localAgent.Repository, agent.ID, request.ID, baseCommit)
			if createErr != nil {
				return protocolv1.Response{}, worktreeFailure(request.ID, createErr)
			}
			invocation.Workspace = record.Path
			invocation.GitCommonDir = record.GitCommonDir
			result := record.Public()
			publicWorktree = &result
		}
		result, runErr := runtimeAdapter.Run(requestCtx, invocation)
		if runErr != nil {
			code, message, retryable := runtimeFailure(runErr)
			return protocolv1.Response{}, &protocolv1.RequestFailure{SchemaVersion: 1, RequestID: request.ID, Code: code, Message: message, Retryable: retryable}
		}
		return protocolv1.Response{SchemaVersion: 1, RequestID: request.ID, Status: protocolv1.StatusSucceeded, Answer: result.FinalMessage, Worktree: publicWorktree}, nil
	})
	if err != nil && !wroteReady {
		return writeMappedError(stderr, err)
	}
	if err != nil {
		return clioutput.ExitConnection
	}
	return clioutput.ExitOK
}

func agentAccess(ctx context.Context, args []string, stdout, stderr io.Writer) clioutput.ExitCode {
	if len(args) < 2 || (args[0] != "grant" && args[0] != "revoke") {
		return writeCLIError(stderr, usageError("agent_access_arguments", "Use: peerctx agent access grant|revoke AGENT --member MEMBER_ID --modes read,write."))
	}
	grant := args[0] == "grant"
	agentID := args[1]
	flags := newFlags("agent access")
	member := flags.String("member", "", "")
	modesText := flags.String("modes", "read", "")
	if err := flags.Parse(args[2:]); err != nil || *member == "" {
		return writeCLIError(stderr, usageError("agent_access_arguments", "Use: peerctx agent access grant|revoke AGENT --member MEMBER_ID --modes read,write."))
	}
	modes, err := parseModes(*modesText)
	if err != nil {
		return writeMappedError(stderr, err)
	}
	_, _, client, err := currentClient()
	if err != nil {
		return writeMappedError(stderr, err)
	}
	acl, err := client.SetAccess(ctx, agentID, *member, modes, grant)
	if err != nil {
		return writeMappedError(stderr, err)
	}
	return writeSuccess(stdout, acl, "")
}

func runAsk(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) clioutput.ExitCode {
	if len(args) == 0 {
		return writeCLIError(stderr, usageError("ask_arguments", "Use: peerctx ask AGENT --mode read [--timeout 5m] [--request-id ID]."))
	}
	agentName := args[0]
	flags := newFlags("ask")
	modeText := flags.String("mode", "", "")
	timeout := flags.Duration("timeout", 5*time.Minute, "")
	requestID := flags.String("request-id", "", "")
	if err := flags.Parse(args[1:]); err != nil || *modeText == "" || *timeout <= 0 {
		return writeCLIError(stderr, usageError("ask_arguments", "Use: peerctx ask AGENT --mode read [--timeout 5m] [--request-id ID]."))
	}
	mode := protocolv1.RequestMode(*modeText)
	if mode != protocolv1.ModeRead {
		return writeCLIError(stderr, usageError("read_mode_required", "M3 supports only: peerctx ask AGENT --mode read."))
	}
	body, err := io.ReadAll(io.LimitReader(stdin, protocolv1.MaxRequestBodyBytes+1))
	if err != nil {
		return writeMappedError(stderr, err)
	}
	if len(body) > protocolv1.MaxRequestBodyBytes {
		return writeCLIError(stderr, &commandError{exit: clioutput.ExitProtocol, errorType: "protocol", subtype: "request", code: "request_too_large", message: "The request body exceeds 256 KiB.", hint: "Send a smaller request body."})
	}
	_, profile, client, err := currentClient()
	if err != nil {
		return writeMappedError(stderr, err)
	}
	agent, err := client.GetAgent(ctx, agentName)
	if err != nil {
		return writeMappedError(stderr, err)
	}
	if *requestID == "" {
		*requestID, err = newRequestID()
		if err != nil {
			return writeMappedError(stderr, err)
		}
	}
	now := time.Now().UTC()
	expires := now.Add(*timeout)
	request := protocolv1.Request{SchemaVersion: 1, ID: *requestID, ProjectID: profile.ProjectID, RequesterID: profile.MemberID, AgentID: agent.ID, Mode: mode, Body: body, BodySHA256: protocolv1.BodySHA256(body), CreatedAt: now, ExpiresAt: &expires}
	callCtx, cancel := context.WithTimeout(ctx, *timeout+5*time.Second)
	defer cancel()
	result, err := client.Ask(callCtx, request)
	if err != nil {
		return writeMappedError(stderr, err)
	}
	return writeSuccess(stdout, result, request.ID)
}

func runTask(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) clioutput.ExitCode {
	if len(args) == 0 {
		return writeCLIError(stderr, usageError("task_arguments", "Use: peerctx task AGENT --mode write [--confirm TOKEN] [--approval-timeout 10m] [--run-timeout 15m] [--request-id ID]."))
	}
	agentName := args[0]
	flags := newFlags("task")
	modeText := flags.String("mode", "", "")
	confirmationToken := flags.String("confirm", "", "")
	approvalTimeout := flags.Duration("approval-timeout", 10*time.Minute, "")
	runTimeout := flags.Duration("run-timeout", 15*time.Minute, "")
	requestID := flags.String("request-id", "", "")
	if err := flags.Parse(args[1:]); err != nil || *modeText == "" || *approvalTimeout <= 0 || *runTimeout <= 0 {
		return writeCLIError(stderr, usageError("task_arguments", "Use: peerctx task AGENT --mode write [--confirm TOKEN] [--approval-timeout 10m] [--run-timeout 15m] [--request-id ID]."))
	}
	if protocolv1.RequestMode(*modeText) != protocolv1.ModeWrite {
		return writeCLIError(stderr, usageError("write_mode_required", "task requires the explicit flag --mode write."))
	}
	body, err := io.ReadAll(io.LimitReader(stdin, protocolv1.MaxRequestBodyBytes+1))
	if err != nil {
		return writeMappedError(stderr, err)
	}
	if len(body) > protocolv1.MaxRequestBodyBytes {
		return writeCLIError(stderr, &commandError{exit: clioutput.ExitProtocol, errorType: "protocol", subtype: "request", code: "request_too_large", message: "The request body exceeds 256 KiB.", hint: "Send a smaller request body."})
	}
	_, profile, client, err := currentClient()
	if err != nil {
		return writeMappedError(stderr, err)
	}
	agent, err := client.GetAgent(ctx, agentName)
	if err != nil {
		return writeMappedError(stderr, err)
	}
	hash := protocolv1.BodySHA256(body)
	if *confirmationToken == "" {
		confirmation := protocolv1.WriteConfirmation{SchemaVersion: 1, AgentID: agent.ID, Mode: protocolv1.ModeWrite, BodyBytes: len(body), BodySHA256: hash, ExpiresAt: time.Now().UTC().Add(*approvalTimeout)}
		token, encodeErr := encodeWriteConfirmation(confirmation)
		if encodeErr != nil {
			return writeMappedError(stderr, encodeErr)
		}
		return writeCLIError(stderr, &commandError{exit: clioutput.ExitConfirmationRequired, errorType: "approval", subtype: "requester_confirmation", code: "write_confirmation_required", message: "The write request has not been sent and requires requester confirmation.", hint: "Ask the requester to approve this exact Agent, mode, byte count, hash, and expiry; then rerun with --confirm.", details: map[string]any{"confirmation": confirmation, "confirmation_token": token}})
	}
	confirmation, err := decodeWriteConfirmation(*confirmationToken)
	if err != nil || confirmation.Validate(time.Now().UTC()) != nil || confirmation.AgentID != agent.ID || confirmation.Mode != protocolv1.ModeWrite || confirmation.BodyBytes != len(body) || confirmation.BodySHA256 != hash {
		return writeCLIError(stderr, &commandError{exit: clioutput.ExitAuthorization, errorType: "authorization", subtype: "requester_confirmation", code: "write_confirmation_mismatch", message: "The confirmation does not match this exact write request or has expired.", hint: "Run the command without --confirm and ask the requester to approve the new confirmation envelope."})
	}
	if *requestID == "" {
		*requestID, err = newRequestID()
		if err != nil {
			return writeMappedError(stderr, err)
		}
	}
	now := time.Now().UTC()
	expires := confirmation.ExpiresAt
	request := protocolv1.Request{SchemaVersion: 1, ID: *requestID, ProjectID: profile.ProjectID, RequesterID: profile.MemberID, AgentID: agent.ID, Mode: protocolv1.ModeWrite, Body: body, BodySHA256: hash, CreatedAt: now, ExpiresAt: &expires}
	remainingApproval := time.Until(expires)
	if remainingApproval < 0 {
		remainingApproval = 0
	}
	callCtx, cancel := context.WithTimeout(ctx, remainingApproval+*runTimeout+5*time.Second)
	defer cancel()
	result, err := client.Submit(callCtx, request)
	if err != nil {
		return writeMappedError(stderr, err)
	}
	return writeSuccess(stdout, result, request.ID)
}

func encodeWriteConfirmation(confirmation protocolv1.WriteConfirmation) (string, error) {
	encoded, err := json.Marshal(confirmation)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

func decodeWriteConfirmation(value string) (protocolv1.WriteConfirmation, error) {
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return protocolv1.WriteConfirmation{}, err
	}
	var confirmation protocolv1.WriteConfirmation
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&confirmation); err != nil {
		return protocolv1.WriteConfirmation{}, err
	}
	return confirmation, nil
}

func runRequest(ctx context.Context, args []string, stdout, stderr io.Writer) clioutput.ExitCode {
	if len(args) == 0 {
		return writeCLIError(stderr, usageError("request_arguments", "Use: peerctx request pending|get|approve|deny|cancel."))
	}
	_, _, client, err := currentClient()
	if err != nil {
		return writeMappedError(stderr, err)
	}
	if args[0] == "pending" {
		if len(args) != 1 {
			return writeCLIError(stderr, usageError("request_pending_arguments", "Use: peerctx request pending."))
		}
		pending, pendingErr := client.PendingRequests(ctx)
		if pendingErr != nil {
			return writeMappedError(stderr, pendingErr)
		}
		return writeSuccess(stdout, map[string]any{"schema_version": 1, "requests": pending}, "")
	}
	if len(args) < 2 {
		return writeCLIError(stderr, usageError("request_arguments", "Use: peerctx request get|approve|deny|cancel REQUEST_ID."))
	}
	requestID := args[1]
	var metadata protocolv1.RequestMetadata
	switch args[0] {
	case "get":
		if len(args) != 2 {
			return writeCLIError(stderr, usageError("request_get_arguments", "Use: peerctx request get REQUEST_ID."))
		}
		metadata, err = client.GetRequest(ctx, requestID)
	case "cancel":
		if len(args) != 2 {
			return writeCLIError(stderr, usageError("request_cancel_arguments", "Use: peerctx request cancel REQUEST_ID."))
		}
		metadata, err = client.CancelRequest(ctx, requestID)
	case "approve":
		flags := newFlags("request approve")
		commit := flags.String("commit", "", "")
		if flags.Parse(args[2:]) != nil || *commit == "" {
			return writeCLIError(stderr, usageError("request_approve_arguments", "Use: peerctx request approve REQUEST_ID --commit COMMIT."))
		}
		metadata, err = client.ApproveRequest(ctx, requestID, *commit)
	case "deny":
		if len(args) != 2 {
			return writeCLIError(stderr, usageError("request_deny_arguments", "Use: peerctx request deny REQUEST_ID."))
		}
		metadata, err = client.DenyRequest(ctx, requestID)
	default:
		return writeCLIError(stderr, usageError("request_arguments", "Use: peerctx request pending|get|approve|deny|cancel."))
	}
	if err != nil {
		return writeMappedError(stderr, err)
	}
	return writeSuccess(stdout, metadata, metadata.ID)
}

func runWorktree(args []string, stdout, stderr io.Writer) clioutput.ExitCode {
	if len(args) == 0 {
		return writeCLIError(stderr, usageError("worktree_command", "Use: peerctx worktree list|remove."))
	}
	local, err := localstate.DefaultManager()
	if err != nil {
		return writeMappedError(stderr, err)
	}
	manager, err := worktree.New(local.Directory())
	if err != nil {
		return writeWorktreeError(stderr, err)
	}
	switch args[0] {
	case "list":
		if len(args) != 1 {
			return writeCLIError(stderr, usageError("worktree_list_arguments", "Use: peerctx worktree list."))
		}
		records, listErr := manager.List()
		if listErr != nil {
			return writeWorktreeError(stderr, listErr)
		}
		return writeSuccess(stdout, map[string]any{"schema_version": 1, "worktrees": records}, "")
	case "remove":
		if len(args) != 2 {
			return writeCLIError(stderr, usageError("worktree_remove_arguments", "Use: peerctx worktree remove WORKTREE_ID."))
		}
		record, removeErr := manager.Remove(args[1])
		if removeErr != nil {
			return writeWorktreeError(stderr, removeErr)
		}
		return writeSuccess(stdout, map[string]any{"schema_version": 1, "worktree_id": record.ID, "path": record.Path, "removed": true, "recoverable": false}, "")
	default:
		return writeCLIError(stderr, usageError("worktree_command", "Use: peerctx worktree list|remove."))
	}
}

func runSkills(args []string, stdout, stderr io.Writer) clioutput.ExitCode {
	if len(args) == 1 && args[0] == "list" {
		return writeSuccess(stdout, map[string]any{"schema_version": 1, "skills": []map[string]any{{"name": skillbundle.Name, "version": version.Current, "files": skillbundle.Paths(), "implicit_invocation": false}}}, "")
	}
	if len(args) >= 2 && args[0] == "read" && args[1] == skillbundle.Name {
		flags := newFlags("skills read")
		file := flags.String("file", "", "")
		if flags.Parse(args[2:]) != nil {
			return writeCLIError(stderr, usageError("skills_read_arguments", "Use: peerctx skills read peer-context [--file PATH]."))
		}
		paths := skillbundle.Paths()
		if *file != "" {
			paths = []string{*file}
		}
		files := make([]map[string]string, 0, len(paths))
		for _, path := range paths {
			content, err := skillbundle.Read(path)
			if err != nil {
				return writeCLIError(stderr, &commandError{exit: clioutput.ExitNotFound, errorType: "not_found", subtype: "skill_file", code: "skill_file_not_found", message: "The embedded Skill file does not exist.", hint: "Run peerctx skills read peer-context without --file to list all embedded files."})
			}
			files = append(files, map[string]string{"path": path, "content": string(content)})
		}
		return writeSuccess(stdout, map[string]any{"schema_version": 1, "name": skillbundle.Name, "version": version.Current, "files": files}, "")
	}
	return writeCLIError(stderr, usageError("skills_command", "Use: peerctx skills list or peerctx skills read peer-context [--file PATH]."))
}

func runDoctor(ctx context.Context, args []string, stdout, stderr io.Writer) clioutput.ExitCode {
	if len(args) != 0 {
		return writeCLIError(stderr, usageError("doctor_arguments", "Use: peerctx doctor."))
	}
	manager, err := localstate.DefaultManager()
	if err != nil {
		return writeMappedError(stderr, err)
	}
	report := doctor.Run(ctx, manager)
	if report.Healthy {
		return writeSuccess(stdout, report, "")
	}
	failedCode := "doctor_failed"
	exit := clioutput.ExitConfiguration
	errorType := "configuration"
	retryable := false
	for _, check := range report.Checks {
		if check.Status != doctor.Fail {
			continue
		}
		failedCode = check.Code
		retryable = check.Retryable
		switch {
		case strings.HasPrefix(check.Code, "runtime_"), strings.HasPrefix(check.Code, "codex_"), strings.HasPrefix(check.Code, "permission_profile"):
			exit = clioutput.ExitRuntime
			errorType = "runtime"
		case check.Code == "relay_unavailable":
			exit = clioutput.ExitConnection
			errorType = "transport"
		case check.Code == "agent_repository_unavailable":
			exit = clioutput.ExitUnavailable
			errorType = "availability"
		}
		break
	}
	return writeCLIError(stderr, &commandError{exit: exit, errorType: errorType, subtype: "doctor", code: failedCode, message: "PeerContext doctor found a blocking prerequisite failure.", hint: "Inspect error.details.checks and fix every failed check before agent serve.", retryable: retryable, details: report})
}

func worktreeFailure(requestID string, err error) *protocolv1.RequestFailure {
	code := "worktree_create_failed"
	var worktreeErr *worktree.Error
	if errors.As(err, &worktreeErr) {
		code = worktreeErr.Code
	}
	return &protocolv1.RequestFailure{SchemaVersion: 1, RequestID: requestID, Code: code, Message: "The approved detached worktree could not be prepared.", Retryable: false}
}

func writeWorktreeError(stderr io.Writer, err error) clioutput.ExitCode {
	code := "worktree_operation_failed"
	var worktreeErr *worktree.Error
	if errors.As(err, &worktreeErr) {
		code = worktreeErr.Code
	}
	exit := clioutput.ExitRuntime
	if code == "worktree_not_found" {
		exit = clioutput.ExitNotFound
	}
	return writeCLIError(stderr, &commandError{exit: exit, errorType: "runtime", subtype: "git_worktree", code: code, message: "The detached worktree operation failed.", hint: "Check Git, the explicit base commit, and provider-local worktree state."})
}

func newRequestID() (string, error) {
	var raw [18]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "req_" + base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func runtimeFailure(err error) (string, string, bool) {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "request_canceled", "The Codex invocation was canceled.", false
	}
	var runtimeErr *codex.RuntimeError
	if errors.As(err, &runtimeErr) {
		retryable := runtimeErr.Code == "codex_execution_failed" || runtimeErr.Code == "isolated_runtime_unavailable"
		return runtimeErr.Code, runtimeErr.Message, retryable
	}
	return "codex_execution_failed", "The isolated Codex invocation failed.", true
}

func writeRuntimeError(stderr io.Writer, err error) clioutput.ExitCode {
	code, message, retryable := runtimeFailure(err)
	exit := clioutput.ExitRuntime
	if code == "codex_protocol_error" || code == "response_too_large" {
		exit = clioutput.ExitProtocol
	}
	if code == "agent_repository_unavailable" {
		exit = clioutput.ExitUnavailable
	}
	return writeCLIError(stderr, &commandError{exit: exit, errorType: "runtime", subtype: "isolated_runtime", code: code, message: message, hint: "Run the isolated Runtime gate and verify Codex authentication and Agent repository configuration.", retryable: retryable})
}

func currentClient() (*localstate.Manager, localstate.Profile, *relayclient.Client, error) {
	manager, err := localstate.DefaultManager()
	if err != nil {
		return nil, localstate.Profile{}, nil, err
	}
	_, profile, err := manager.Current()
	if err != nil {
		return nil, localstate.Profile{}, nil, err
	}
	token, err := manager.Token(profile)
	if err != nil {
		return nil, localstate.Profile{}, nil, err
	}
	client, err := relayclient.New(profile.RelayURL, token)
	return manager, profile, client, err
}
func newFlags(name string) *flag.FlagSet {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	return flags
}
func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return []string{}
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
func parseModes(value string) ([]protocolv1.RequestMode, error) {
	parts := splitCSV(value)
	modes := make([]protocolv1.RequestMode, 0, len(parts))
	for _, part := range parts {
		mode := protocolv1.RequestMode(part)
		if err := mode.Validate(); err != nil {
			return nil, err
		}
		modes = append(modes, mode)
	}
	if len(modes) == 0 {
		return nil, errors.New("at least one mode is required")
	}
	return modes, nil
}
func credentialStoreName(file string) string {
	if file != "" {
		return "file"
	}
	return "keyring"
}
func defaultRelayDatabase() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "peerctx-relay.sqlite"
	}
	return filepath.Join(dir, "peerctx", "relay.sqlite")
}
func writeSuccess(stdout io.Writer, data any, requestID string) clioutput.ExitCode {
	if err := clioutput.WriteSuccess(stdout, data, clioutput.Meta{RequestID: requestID, Version: version.Current}); err != nil {
		return clioutput.ExitInternal
	}
	return clioutput.ExitOK
}
func usageError(code, hint string) error {
	return &commandError{exit: clioutput.ExitUsage, errorType: "usage", subtype: "command", code: code, message: "Invalid peerctx command or arguments.", hint: hint}
}

type commandError struct {
	exit                                    clioutput.ExitCode
	errorType, subtype, code, message, hint string
	retryable                               bool
	details                                 any
}

func (e *commandError) Error() string { return e.message }
func writeMappedError(stderr io.Writer, err error) clioutput.ExitCode {
	var command *commandError
	if errors.As(err, &command) {
		return writeCLIError(stderr, err)
	}
	var remote *relayclient.Error
	if errors.As(err, &remote) {
		exit := clioutput.ExitConnection
		errorType := "transport"
		switch remote.Code {
		case "agent_offline":
			exit = clioutput.ExitUnavailable
			errorType = "availability"
		case "agent_access_denied", "agent_access_revoked":
			exit = clioutput.ExitAuthorization
			errorType = "authorization"
		case "request_timeout", "request_expired":
			exit = clioutput.ExitTimeout
			errorType = "timeout"
		case "request_canceled":
			exit = clioutput.ExitCanceled
			errorType = "canceled"
		case "write_request_denied":
			exit = clioutput.ExitDenied
			errorType = "approval"
		case "write_approval_expired":
			exit = clioutput.ExitTimeout
			errorType = "timeout"
		case "requester_credential_revoked":
			exit = clioutput.ExitAuthentication
			errorType = "authentication"
		case "codex_protocol_error", "provider_protocol_error", "response_too_large":
			exit = clioutput.ExitProtocol
			errorType = "protocol"
		case "agent_repository_unavailable":
			exit = clioutput.ExitUnavailable
			errorType = "availability"
		case "codex_execution_failed", "isolated_runtime_unavailable", "provider_runtime_unavailable", "codex_auth_unavailable", "codex_version_unavailable", "codex_interface_unsupported", "codex_read_isolation_probe_failed", "codex_write_isolation_probe_failed", "git_unavailable", "base_commit_invalid", "worktree_create_failed", "worktree_verification_failed", "worktree_not_detached", "git_metadata_boundary_invalid", "git_metadata_boundary_required":
			exit = clioutput.ExitRuntime
			errorType = "runtime"
		}
		switch remote.Status {
		case 400:
			if exit == clioutput.ExitConnection {
				exit = clioutput.ExitUsage
				errorType = "usage"
			}
		case 401:
			exit = clioutput.ExitAuthentication
			errorType = "authentication"
		case 403:
			exit = clioutput.ExitAuthorization
			errorType = "authorization"
		case 404:
			exit = clioutput.ExitNotFound
			errorType = "not_found"
		case 409:
			if exit == clioutput.ExitConnection {
				exit = clioutput.ExitConflict
				errorType = "conflict"
			}
		case 410:
			exit = clioutput.ExitTimeout
			errorType = "timeout"
		case 408:
			exit = clioutput.ExitTimeout
			errorType = "timeout"
		}
		return writeCLIError(stderr, &commandError{exit: exit, errorType: errorType, subtype: "relay", code: remote.Code, message: remote.Message, hint: "Check the Relay, project identity, and command arguments.", retryable: remote.Retryable || remote.Status >= 500})
	}
	var transport *relayclient.TransportError
	if errors.As(err, &transport) {
		return writeCLIError(stderr, &commandError{exit: clioutput.ExitConnection, errorType: "transport", subtype: "relay", code: "relay_connection_failed", message: "The Relay connection failed.", hint: "Check the Relay URL, TLS trust, network, and Relay availability.", retryable: true})
	}
	return writeCLIError(stderr, &commandError{exit: clioutput.ExitConfiguration, errorType: "configuration", subtype: "local", code: "local_configuration_error", message: err.Error(), hint: "Check the local PeerContext configuration and credential storage."})
}
func writeCLIError(stderr io.Writer, err error) clioutput.ExitCode {
	command, ok := err.(*commandError)
	if !ok {
		command = &commandError{exit: clioutput.ExitInternal, errorType: "internal", subtype: "command", code: "internal_error", message: err.Error(), hint: "Retry or inspect the local setup."}
	}
	apiErr := clioutput.NewError(command.exit, command.errorType, command.subtype, command.code, command.message, command.hint, command.retryable)
	if command.details != nil {
		apiErr = apiErr.WithDetails(command.details)
	}
	if writeErr := clioutput.WriteError(stderr, apiErr); writeErr != nil {
		return clioutput.ExitInternal
	}
	return command.exit
}

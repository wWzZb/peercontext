package cli

import (
	"context"
	"errors"
	"flag"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/wWzZb/peercontext/internal/failure"
	"github.com/wWzZb/peercontext/internal/service"
	"github.com/wWzZb/peercontext/internal/skillbundle"
	"github.com/wWzZb/peercontext/internal/v2state"
	"github.com/wWzZb/peercontext/internal/version"
	"github.com/wWzZb/peercontext/pkg/clioutput"
	protocolv2 "github.com/wWzZb/peercontext/pkg/protocol/v2"
)

func runV2(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, jsonOutput bool) clioutput.ExitCode {
	if args[0] == "ask" && os.Getenv("PEERCTX_INBOUND_REQUEST") != "" {
		return writeCLIError(stderr, clioutput.ExitAuthorization, "recursive_request_blocked", "Inbound Codex requests cannot create another PeerContext request.", "Return the result without calling peerctx ask.", false, jsonOutput)
	}
	switch args[0] {
	case "project":
		return runProject(ctx, args[1:], stdout, stderr, jsonOutput)
	case "agent":
		return runAgent(ctx, args[1:], stdout, stderr, jsonOutput)
	case "ask":
		return runAsk(ctx, args[1:], stdin, stdout, stderr, jsonOutput)
	case "service":
		return runService(ctx, args[1:], stdout, stderr, jsonOutput)
	case "skills":
		return runSkills(args[1:], stdout, stderr, jsonOutput)
	default:
		return writeCLIError(stderr, clioutput.ExitUsage, "unknown_command", "Unknown PeerContext command.", "Run peerctx --help to see available commands.", false, jsonOutput)
	}
}

func runProject(ctx context.Context, args []string, stdout, stderr io.Writer, jsonOutput bool) clioutput.ExitCode {
	if len(args) == 0 {
		return usage(stderr, "Use: peerctx project create|join|list|use|invite|member.", jsonOutput)
	}
	switch args[0] {
	case "create":
		flags := newFlags("project create")
		name := flags.String("name", "", "")
		member := flags.String("member", "", "")
		if flags.Parse(args[1:]) != nil || *name == "" || flags.NArg() != 0 {
			return usage(stderr, "Use: peerctx project create --name NAME [--member NAME].", jsonOutput)
		}
		memberName := chooseMemberName(*member)
		var result service.ProjectCreateResult
		if err := control(ctx, service.ActionProjectCreate, service.ProjectCreateInput{Name: *name, MemberName: memberName}, &result); err != nil {
			return writeMappedError(stderr, err, jsonOutput)
		}
		invitation, _ := protocolv2.DecodeInvitation(result.Invitation, time.Time{})
		data := projectCreatedData{SchemaVersion: 2, Project: result.Project, Member: result.Member, Invitation: result.Invitation, ExpiresAt: invitation.ExpiresAt}
		return writeSuccess(stdout, data, "", "project.create", jsonOutput)
	case "join":
		if len(args) < 2 {
			return usage(stderr, "Use: peerctx project join INVITATION [--member NAME].", jsonOutput)
		}
		invitation := args[1]
		flags := newFlags("project join")
		member := flags.String("member", "", "")
		if flags.Parse(args[2:]) != nil || flags.NArg() != 0 {
			return usage(stderr, "Use: peerctx project join INVITATION [--member NAME].", jsonOutput)
		}
		var result service.ProjectJoinResult
		if err := control(ctx, service.ActionProjectJoin, service.ProjectJoinInput{Invitation: invitation, MemberName: chooseMemberName(*member)}, &result); err != nil {
			return writeMappedError(stderr, err, jsonOutput)
		}
		return writeSuccess(stdout, projectJoinedData{SchemaVersion: 2, Project: result.Project, Member: result.Member}, "", "project.join", jsonOutput)
	case "list":
		var result projectListData
		if err := control(ctx, service.ActionProjectList, struct{}{}, &result); err != nil {
			return writeMappedError(stderr, err, jsonOutput)
		}
		return writeSuccess(stdout, result, "", "project.list", jsonOutput)
	case "use":
		if len(args) != 2 {
			return usage(stderr, "Use: peerctx project use PROJECT_ID.", jsonOutput)
		}
		var result map[string]any
		if err := control(ctx, service.ActionProjectUse, service.ProjectUseInput{ProjectID: args[1]}, &result); err != nil {
			return writeMappedError(stderr, err, jsonOutput)
		}
		return writeSuccess(stdout, result, "", "project.use", jsonOutput)
	case "invite":
		if len(args) != 2 || args[1] != "create" {
			return usage(stderr, "Use: peerctx project invite create.", jsonOutput)
		}
		var result invitationData
		if err := control(ctx, service.ActionInviteCreate, struct{}{}, &result); err != nil {
			return writeMappedError(stderr, err, jsonOutput)
		}
		return writeSuccess(stdout, result, "", "project.invite.create", jsonOutput)
	case "member":
		if len(args) == 2 && args[1] == "list" {
			var members []protocolv2.Member
			if err := control(ctx, service.ActionMemberList, struct{}{}, &members); err != nil {
				return writeMappedError(stderr, err, jsonOutput)
			}
			return writeSuccess(stdout, membersData{SchemaVersion: 2, Members: members}, "", "project.member.list", jsonOutput)
		}
		if len(args) == 3 && args[1] == "remove" {
			var result map[string]bool
			if err := control(ctx, service.ActionMemberRemove, service.MemberRemoveInput{MemberID: args[2]}, &result); err != nil {
				return writeMappedError(stderr, err, jsonOutput)
			}
			return writeSuccess(stdout, result, "", "project.member.remove", jsonOutput)
		}
		return usage(stderr, "Use: peerctx project member list|remove MEMBER_ID.", jsonOutput)
	default:
		return usage(stderr, "Use: peerctx project create|join|list|use|invite|member.", jsonOutput)
	}
}

func runAgent(ctx context.Context, args []string, stdout, stderr io.Writer, jsonOutput bool) clioutput.ExitCode {
	if len(args) == 0 {
		return usage(stderr, "Use: peerctx agent register|list|get|remove.", jsonOutput)
	}
	switch args[0] {
	case "register":
		if len(args) < 2 {
			return usage(stderr, "Use: peerctx agent register REPOSITORY [--name NAME] [--summary TEXT] [--tags CSV] [--capabilities CSV].", jsonOutput)
		}
		flags := newFlags("agent register")
		name := flags.String("name", "", "")
		summary := flags.String("summary", "", "")
		tags := flags.String("tags", "", "")
		capabilities := flags.String("capabilities", "", "")
		if flags.Parse(args[2:]) != nil || flags.NArg() != 0 {
			return usage(stderr, "Use: peerctx agent register REPOSITORY [--name NAME] [--summary TEXT] [--tags CSV] [--capabilities CSV].", jsonOutput)
		}
		var result protocolv2.Agent
		input := service.AgentRegisterInput{Repository: args[1], Name: *name, Summary: *summary, Tags: splitCSV(*tags), Capabilities: splitCSV(*capabilities)}
		if err := control(ctx, service.ActionAgentRegister, input, &result); err != nil {
			return writeMappedError(stderr, err, jsonOutput)
		}
		return writeSuccess(stdout, result, "", "agent.register", jsonOutput)
	case "list":
		var agents []protocolv2.Agent
		if err := control(ctx, service.ActionAgentList, struct{}{}, &agents); err != nil {
			return writeMappedError(stderr, err, jsonOutput)
		}
		return writeSuccess(stdout, agentsData{SchemaVersion: 2, Agents: agents}, "", "agent.list", jsonOutput)
	case "get", "remove":
		if len(args) != 2 {
			return usage(stderr, "Use: peerctx agent "+args[0]+" AGENT.", jsonOutput)
		}
		action := service.ActionAgentGet
		var output any = &protocolv2.Agent{}
		if args[0] == "remove" {
			action = service.ActionAgentRemove
			output = &map[string]bool{}
		}
		if err := control(ctx, action, service.AgentSelectorInput{Agent: args[1]}, output); err != nil {
			return writeMappedError(stderr, err, jsonOutput)
		}
		return writeSuccess(stdout, output, "", "agent."+args[0], jsonOutput)
	default:
		return usage(stderr, "Use: peerctx agent register|list|get|remove.", jsonOutput)
	}
}

func runAsk(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, jsonOutput bool) clioutput.ExitCode {
	if len(args) < 1 {
		return usage(stderr, "Use: peerctx ask AGENT [--timeout 5m] [--request-id ID].", jsonOutput)
	}
	flags := newFlags("ask")
	timeout := flags.Duration("timeout", protocolv2.DefaultRequestTimeout, "")
	requestID := flags.String("request-id", "", "")
	if flags.Parse(args[1:]) != nil || flags.NArg() != 0 || *timeout <= 0 {
		return usage(stderr, "Use: peerctx ask AGENT [--timeout 5m] [--request-id ID].", jsonOutput)
	}
	body, err := io.ReadAll(io.LimitReader(stdin, protocolv2.MaxRequestBodyBytes+1))
	if err != nil || len(body) > protocolv2.MaxRequestBodyBytes {
		return writeCLIError(stderr, clioutput.ExitUsage, "request_too_large", "Request body is too large.", "Keep stdin at or below 256 KiB.", false, jsonOutput)
	}
	var response protocolv2.Response
	err = control(ctx, service.ActionAsk, service.AskInput{Agent: args[0], RequestID: *requestID, Body: body, TimeoutMS: timeout.Milliseconds()}, &response)
	if err != nil {
		return writeMappedError(stderr, err, jsonOutput)
	}
	result := protocolv2.AskResult{SchemaVersion: protocolv2.SchemaVersion, Response: &response, Replayed: false}
	return writeSuccess(stdout, result, response.RequestID, "ask", jsonOutput)
}

func runService(ctx context.Context, args []string, stdout, stderr io.Writer, jsonOutput bool) clioutput.ExitCode {
	if len(args) != 1 {
		return usage(stderr, "Use: peerctx service start|stop|restart|status.", jsonOutput)
	}
	manager, err := v2state.DefaultManager()
	if err != nil {
		return writeMappedError(stderr, err, jsonOutput)
	}
	launchAgent, err := service.DefaultLaunchAgent(manager)
	if err != nil {
		return writeMappedError(stderr, err, jsonOutput)
	}
	switch args[0] {
	case "start":
		err = launchAgent.Ensure(ctx)
	case "stop":
		err = launchAgent.Stop(ctx)
	case "restart":
		err = launchAgent.Restart(ctx)
	case "status":
		var status map[string]any
		status, err = launchAgent.Status(ctx)
		if err == nil {
			return writeSuccess(stdout, status, "", "service.status", jsonOutput)
		}
	default:
		return usage(stderr, "Use: peerctx service start|stop|restart|status.", jsonOutput)
	}
	if err != nil {
		return writeMappedError(stderr, err, jsonOutput)
	}
	return writeSuccess(stdout, map[string]any{"schema_version": 2, "action": args[0], "ok": true}, "", "service.action", jsonOutput)
}

func runSkills(args []string, stdout, stderr io.Writer, jsonOutput bool) clioutput.ExitCode {
	if len(args) == 1 && args[0] == "list" {
		return writeSuccess(stdout, map[string]any{"schema_version": 2, "skills": []map[string]any{{"name": skillbundle.Name, "version": version.Current, "files": skillbundle.Paths(), "implicit_invocation": false}}}, "", "skills.list", jsonOutput)
	}
	if len(args) >= 2 && args[0] == "read" && args[1] == skillbundle.Name {
		flags := newFlags("skills read")
		file := flags.String("file", "", "")
		if flags.Parse(args[2:]) != nil {
			return usage(stderr, "Use: peerctx skills read peer-context [--file PATH].", jsonOutput)
		}
		paths := skillbundle.Paths()
		if *file != "" {
			paths = []string{*file}
		}
		files := make([]map[string]string, 0, len(paths))
		for _, path := range paths {
			content, err := skillbundle.Read(path)
			if err != nil {
				return writeMappedError(stderr, err, jsonOutput)
			}
			files = append(files, map[string]string{"path": path, "content": string(content)})
		}
		return writeSuccess(stdout, map[string]any{"schema_version": 2, "name": skillbundle.Name, "version": version.Current, "files": files}, "", "skills.read", jsonOutput)
	}
	return usage(stderr, "Use: peerctx skills list or peerctx skills read peer-context [--file PATH].", jsonOutput)
}

func control(ctx context.Context, action string, input, output any) error {
	manager, err := v2state.DefaultManager()
	if err != nil {
		return err
	}
	launchAgent, err := service.DefaultLaunchAgent(manager)
	if err != nil {
		return err
	}
	if err := launchAgent.Ensure(ctx); err != nil {
		return err
	}
	return service.NewControlClient(manager.SocketPath()).Do(ctx, action, input, output)
}

func chooseMemberName(override string) string {
	if value := strings.TrimSpace(override); value != "" {
		return value
	}
	if output, err := exec.Command("git", "config", "--global", "user.name").Output(); err == nil {
		if value := strings.TrimSpace(string(output)); value != "" {
			return value
		}
	}
	if value := strings.TrimSpace(os.Getenv("USER")); value != "" {
		return value
	}
	return "member"
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

func writeSuccess(stdout io.Writer, data any, requestID, command string, jsonOutput bool) clioutput.ExitCode {
	var err error
	if jsonOutput {
		err = clioutput.WriteSuccess(stdout, data, clioutput.Meta{RequestID: requestID, Version: version.Current})
	} else {
		err = renderHuman(stdout, command, data)
	}
	if err != nil {
		return clioutput.ExitInternal
	}
	return clioutput.ExitOK
}

func usage(stderr io.Writer, hint string, jsonOutput bool) clioutput.ExitCode {
	return writeCLIError(stderr, clioutput.ExitUsage, "invalid_arguments", "Invalid PeerContext command or arguments.", hint+" Run the command with --help for details.", false, jsonOutput)
}

func writeMappedError(stderr io.Writer, err error, jsonOutput bool) clioutput.ExitCode {
	code := "peerctx_error"
	message := err.Error()
	retryable := false
	if errors.Is(err, context.DeadlineExceeded) {
		code, message, retryable = "request_timeout", "The PeerContext request timed out.", true
	} else {
		var structured *failure.Error
		if errors.As(err, &structured) {
			code, message, retryable = structured.Code, structured.Message, structured.Retryable
		}
	}
	exit, hint := errorPresentation(code)
	return writeCLIError(stderr, exit, code, message, hint, retryable, jsonOutput)
}

func errorPresentation(code string) (clioutput.ExitCode, string) {
	switch code {
	case "invite_expired":
		return clioutput.ExitTimeout, "Ask a Project Owner to create a new invitation."
	case "invite_consumed":
		return clioutput.ExitConflict, "This invitation was already used. Ask a Project Owner to create a new one."
	case "project_host_offline":
		return clioutput.ExitConnection, "Confirm the Project creator's Mac is awake, online, and on the same LAN."
	case "agent_offline", "agent_unavailable":
		return clioutput.ExitUnavailable, "Confirm the Agent owner's Mac is awake and its PeerContext service is running."
	case "host_identity_mismatch":
		return clioutput.ExitProtocol, "Do not continue. Confirm the invitation or saved Project with its Owner."
	case "invalid_invitation":
		return clioutput.ExitProtocol, "Copy the complete peerctx2_ invitation again, or ask the Owner for a new one."
	case "request_replayed":
		return clioutput.ExitProtocol, "Do not retry the same signed request; run the command again to create a fresh nonce."
	case "clock_skew":
		return clioutput.ExitProtocol, "Set both Macs to automatic date and time, then try again."
	case "lan_discovery_unavailable":
		return clioutput.ExitConnection, "Keep both Macs on the same LAN and check whether multicast DNS is allowed."
	case "signature_invalid":
		return clioutput.ExitProtocol, "The signed message could not be trusted. Stop and verify Project membership."
	case "lan_peer_required":
		return clioutput.ExitConnection, "Connect both Macs to the same directly connected LAN."
	case "authorization_failed", "recursive_request_blocked":
		return clioutput.ExitAuthorization, "Check current Project membership and the requested operation."
	case "not_found", "agent_not_found":
		return clioutput.ExitNotFound, "Refresh the relevant list and choose an existing ID or name."
	case "request_timeout":
		return clioutput.ExitTimeout, "Check the Agent and network status before trying again."
	case "request_canceled":
		return clioutput.ExitCanceled, "Run the command again only if the request is still needed."
	case "agent_repository_unavailable", "isolated_runtime_unavailable", "codex_execution_failed", "codex_protocol_error":
		return clioutput.ExitRuntime, "Check the Agent owner's Codex login and repository, then inspect service status."
	default:
		return clioutput.ExitConfiguration, "Run peerctx service status for local diagnostics."
	}
}

func writeCLIError(stderr io.Writer, exit clioutput.ExitCode, code, message, hint string, retryable, jsonOutput bool) clioutput.ExitCode {
	apiErr := clioutput.NewError(exit, "peerctx", "lan_v2", code, message, hint, retryable)
	return writeError(stderr, apiErr, jsonOutput)
}

func writeError(stderr io.Writer, apiErr clioutput.Error, jsonOutput bool) clioutput.ExitCode {
	var err error
	if jsonOutput {
		err = clioutput.WriteError(stderr, apiErr)
	} else {
		_, err = io.WriteString(stderr, "Error ["+apiErr.Code+"]: "+apiErr.Message+"\nNext: "+apiErr.Hint+"\n")
	}
	if err != nil {
		return clioutput.ExitInternal
	}
	return apiErr.ExitCode()
}

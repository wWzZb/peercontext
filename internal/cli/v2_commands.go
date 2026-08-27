package cli

import (
	"context"
	"errors"
	"flag"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/wWzZb/peercontext/internal/service"
	"github.com/wWzZb/peercontext/internal/skillbundle"
	"github.com/wWzZb/peercontext/internal/v2state"
	"github.com/wWzZb/peercontext/internal/version"
	"github.com/wWzZb/peercontext/pkg/clioutput"
	protocolv2 "github.com/wWzZb/peercontext/pkg/protocol/v2"
)

func runV2(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) clioutput.ExitCode {
	if args[0] == "ask" && os.Getenv("PEERCTX_INBOUND_REQUEST") != "" {
		return writeCLIError(stderr, clioutput.ExitAuthorization, "recursive_request_blocked", "Inbound Codex requests cannot create another PeerContext request.", "Return the result without calling peerctx ask.", false)
	}
	switch args[0] {
	case "project":
		return runProject(ctx, args[1:], stdout, stderr)
	case "agent":
		return runAgent(ctx, args[1:], stdout, stderr)
	case "ask":
		return runAsk(ctx, args[1:], stdin, stdout, stderr)
	case "service":
		return runService(ctx, args[1:], stdout, stderr)
	case "skills":
		return runSkills(args[1:], stdout, stderr)
	default:
		return writeCLIError(stderr, clioutput.ExitUsage, "unknown_command", "Unknown PeerContext command.", "Use project, agent, ask, service, skills, or version.", false)
	}
}

func runProject(ctx context.Context, args []string, stdout, stderr io.Writer) clioutput.ExitCode {
	if len(args) == 0 {
		return usage(stderr, "Use: peerctx project create|join|list|use|invite|member.")
	}
	switch args[0] {
	case "create":
		flags := newFlags("project create")
		name := flags.String("name", "", "")
		member := flags.String("member", "", "")
		if flags.Parse(args[1:]) != nil || *name == "" || flags.NArg() != 0 {
			return usage(stderr, "Use: peerctx project create --name NAME [--member NAME].")
		}
		memberName := chooseMemberName(*member)
		var result service.ProjectCreateResult
		if err := control(ctx, service.ActionProjectCreate, service.ProjectCreateInput{Name: *name, MemberName: memberName}, &result); err != nil {
			return writeMappedError(stderr, err)
		}
		return writeSuccess(stdout, map[string]any{"schema_version": 2, "project": result.Project, "member": result.Member, "invitation": result.Invitation}, "")
	case "join":
		if len(args) < 2 {
			return usage(stderr, "Use: peerctx project join INVITATION [--member NAME].")
		}
		invitation := args[1]
		flags := newFlags("project join")
		member := flags.String("member", "", "")
		if flags.Parse(args[2:]) != nil || flags.NArg() != 0 {
			return usage(stderr, "Use: peerctx project join INVITATION [--member NAME].")
		}
		var result service.ProjectJoinResult
		if err := control(ctx, service.ActionProjectJoin, service.ProjectJoinInput{Invitation: invitation, MemberName: chooseMemberName(*member)}, &result); err != nil {
			return writeMappedError(stderr, err)
		}
		return writeSuccess(stdout, map[string]any{"schema_version": 2, "project": result.Project, "member": result.Member}, "")
	case "list":
		var result map[string]any
		if err := control(ctx, service.ActionProjectList, struct{}{}, &result); err != nil {
			return writeMappedError(stderr, err)
		}
		return writeSuccess(stdout, result, "")
	case "use":
		if len(args) != 2 {
			return usage(stderr, "Use: peerctx project use PROJECT_ID.")
		}
		var result map[string]any
		if err := control(ctx, service.ActionProjectUse, service.ProjectUseInput{ProjectID: args[1]}, &result); err != nil {
			return writeMappedError(stderr, err)
		}
		return writeSuccess(stdout, result, "")
	case "invite":
		if len(args) != 2 || args[1] != "create" {
			return usage(stderr, "Use: peerctx project invite create.")
		}
		var result map[string]any
		if err := control(ctx, service.ActionInviteCreate, struct{}{}, &result); err != nil {
			return writeMappedError(stderr, err)
		}
		return writeSuccess(stdout, result, "")
	case "member":
		if len(args) == 2 && args[1] == "list" {
			var members []protocolv2.Member
			if err := control(ctx, service.ActionMemberList, struct{}{}, &members); err != nil {
				return writeMappedError(stderr, err)
			}
			return writeSuccess(stdout, map[string]any{"schema_version": 2, "members": members}, "")
		}
		if len(args) == 3 && args[1] == "remove" {
			var result map[string]bool
			if err := control(ctx, service.ActionMemberRemove, service.MemberRemoveInput{MemberID: args[2]}, &result); err != nil {
				return writeMappedError(stderr, err)
			}
			return writeSuccess(stdout, result, "")
		}
		return usage(stderr, "Use: peerctx project member list|remove MEMBER_ID.")
	default:
		return usage(stderr, "Use: peerctx project create|join|list|use|invite|member.")
	}
}

func runAgent(ctx context.Context, args []string, stdout, stderr io.Writer) clioutput.ExitCode {
	if len(args) == 0 {
		return usage(stderr, "Use: peerctx agent register|list|get|remove.")
	}
	switch args[0] {
	case "register":
		if len(args) < 2 {
			return usage(stderr, "Use: peerctx agent register REPOSITORY [--name NAME] [--summary TEXT] [--tags CSV] [--capabilities CSV].")
		}
		flags := newFlags("agent register")
		name := flags.String("name", "", "")
		summary := flags.String("summary", "", "")
		tags := flags.String("tags", "", "")
		capabilities := flags.String("capabilities", "", "")
		if flags.Parse(args[2:]) != nil || flags.NArg() != 0 {
			return usage(stderr, "Use: peerctx agent register REPOSITORY [--name NAME] [--summary TEXT] [--tags CSV] [--capabilities CSV].")
		}
		var result protocolv2.Agent
		input := service.AgentRegisterInput{Repository: args[1], Name: *name, Summary: *summary, Tags: splitCSV(*tags), Capabilities: splitCSV(*capabilities)}
		if err := control(ctx, service.ActionAgentRegister, input, &result); err != nil {
			return writeMappedError(stderr, err)
		}
		return writeSuccess(stdout, result, "")
	case "list":
		var agents []protocolv2.Agent
		if err := control(ctx, service.ActionAgentList, struct{}{}, &agents); err != nil {
			return writeMappedError(stderr, err)
		}
		return writeSuccess(stdout, map[string]any{"schema_version": 2, "agents": agents}, "")
	case "get", "remove":
		if len(args) != 2 {
			return usage(stderr, "Use: peerctx agent "+args[0]+" AGENT.")
		}
		action := service.ActionAgentGet
		var output any = &protocolv2.Agent{}
		if args[0] == "remove" {
			action = service.ActionAgentRemove
			output = &map[string]bool{}
		}
		if err := control(ctx, action, service.AgentSelectorInput{Agent: args[1]}, output); err != nil {
			return writeMappedError(stderr, err)
		}
		return writeSuccess(stdout, output, "")
	default:
		return usage(stderr, "Use: peerctx agent register|list|get|remove.")
	}
}

func runAsk(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) clioutput.ExitCode {
	if len(args) < 1 {
		return usage(stderr, "Use: peerctx ask AGENT [--timeout 5m] [--request-id ID].")
	}
	flags := newFlags("ask")
	timeout := flags.Duration("timeout", protocolv2.DefaultRequestTimeout, "")
	requestID := flags.String("request-id", "", "")
	if flags.Parse(args[1:]) != nil || flags.NArg() != 0 || *timeout <= 0 {
		return usage(stderr, "Use: peerctx ask AGENT [--timeout 5m] [--request-id ID].")
	}
	body, err := io.ReadAll(io.LimitReader(stdin, protocolv2.MaxRequestBodyBytes+1))
	if err != nil || len(body) > protocolv2.MaxRequestBodyBytes {
		return writeCLIError(stderr, clioutput.ExitUsage, "request_too_large", "Request body is too large.", "Keep stdin at or below 256 KiB.", false)
	}
	var response protocolv2.Response
	err = control(ctx, service.ActionAsk, service.AskInput{Agent: args[0], RequestID: *requestID, Body: body, TimeoutMS: timeout.Milliseconds()}, &response)
	if err != nil {
		return writeMappedError(stderr, err)
	}
	result := protocolv2.AskResult{SchemaVersion: protocolv2.SchemaVersion, Response: &response, Replayed: false}
	return writeSuccess(stdout, result, response.RequestID)
}

func runService(ctx context.Context, args []string, stdout, stderr io.Writer) clioutput.ExitCode {
	if len(args) != 1 {
		return usage(stderr, "Use: peerctx service start|stop|restart|status.")
	}
	manager, err := v2state.DefaultManager()
	if err != nil {
		return writeMappedError(stderr, err)
	}
	launchAgent, err := service.DefaultLaunchAgent(manager)
	if err != nil {
		return writeMappedError(stderr, err)
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
			return writeSuccess(stdout, status, "")
		}
	default:
		return usage(stderr, "Use: peerctx service start|stop|restart|status.")
	}
	if err != nil {
		return writeMappedError(stderr, err)
	}
	return writeSuccess(stdout, map[string]any{"schema_version": 2, "action": args[0], "ok": true}, "")
}

func runSkills(args []string, stdout, stderr io.Writer) clioutput.ExitCode {
	if len(args) == 1 && args[0] == "list" {
		return writeSuccess(stdout, map[string]any{"schema_version": 2, "skills": []map[string]any{{"name": skillbundle.Name, "version": version.Current, "files": skillbundle.Paths(), "implicit_invocation": false}}}, "")
	}
	if len(args) >= 2 && args[0] == "read" && args[1] == skillbundle.Name {
		flags := newFlags("skills read")
		file := flags.String("file", "", "")
		if flags.Parse(args[2:]) != nil {
			return usage(stderr, "Use: peerctx skills read peer-context [--file PATH].")
		}
		paths := skillbundle.Paths()
		if *file != "" {
			paths = []string{*file}
		}
		files := make([]map[string]string, 0, len(paths))
		for _, path := range paths {
			content, err := skillbundle.Read(path)
			if err != nil {
				return writeMappedError(stderr, err)
			}
			files = append(files, map[string]string{"path": path, "content": string(content)})
		}
		return writeSuccess(stdout, map[string]any{"schema_version": 2, "name": skillbundle.Name, "version": version.Current, "files": files}, "")
	}
	return usage(stderr, "Use: peerctx skills list or peerctx skills read peer-context [--file PATH].")
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

func writeSuccess(stdout io.Writer, data any, requestID string) clioutput.ExitCode {
	if err := clioutput.WriteSuccess(stdout, data, clioutput.Meta{RequestID: requestID, Version: version.Current}); err != nil {
		return clioutput.ExitInternal
	}
	return clioutput.ExitOK
}

func usage(stderr io.Writer, hint string) clioutput.ExitCode {
	return writeCLIError(stderr, clioutput.ExitUsage, "invalid_arguments", "Invalid PeerContext command or arguments.", hint, false)
}

func writeMappedError(stderr io.Writer, err error) clioutput.ExitCode {
	exit := clioutput.ExitConfiguration
	code := "peerctx_error"
	retryable := false
	message := err.Error()
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		exit, code = clioutput.ExitTimeout, "request_timeout"
	case strings.Contains(message, "expired"):
		exit, code = clioutput.ExitTimeout, "expired"
	case strings.Contains(message, "already used"), strings.Contains(message, "consumed"), strings.Contains(message, "constraint failed"):
		exit, code = clioutput.ExitConflict, "conflict"
	case strings.Contains(message, "Codex"), strings.Contains(message, "codex"), strings.Contains(message, "isolated"):
		exit, code = clioutput.ExitRuntime, "runtime_failed"
	case strings.Contains(message, "offline"), strings.Contains(message, "unavailable"):
		exit, code, retryable = clioutput.ExitUnavailable, "unavailable", true
	case strings.Contains(message, "not found"):
		exit, code = clioutput.ExitNotFound, "not_found"
	case strings.Contains(message, "unauthorized"), strings.Contains(message, "forbidden"):
		exit, code = clioutput.ExitAuthorization, "authorization_failed"
	case strings.Contains(message, "signature"), strings.Contains(message, "identity"), strings.Contains(message, "invitation"):
		exit, code = clioutput.ExitProtocol, "protocol_verification_failed"
	case strings.Contains(message, "connection"), strings.Contains(message, "LAN"), strings.Contains(message, "mDNS"):
		exit, code, retryable = clioutput.ExitConnection, "lan_connection_failed", true
	}
	return writeCLIError(stderr, exit, code, message, "Check that both Macs are on the same LAN and the Project host is online.", retryable)
}

func writeCLIError(stderr io.Writer, exit clioutput.ExitCode, code, message, hint string, retryable bool) clioutput.ExitCode {
	apiErr := clioutput.NewError(exit, "peerctx", "lan_v2", code, message, hint, retryable)
	if err := clioutput.WriteError(stderr, apiErr); err != nil {
		return clioutput.ExitInternal
	}
	return exit
}

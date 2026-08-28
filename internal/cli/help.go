package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/wWzZb/peercontext/pkg/clioutput"
)

func parseGlobalArgs(args []string) (remaining []string, jsonOutput bool) {
	remaining = make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "--json" {
			jsonOutput = true
			continue
		}
		remaining = append(remaining, arg)
	}
	return remaining, jsonOutput
}

func wantsHelp(args []string) bool {
	if len(args) == 0 || args[0] == "help" {
		return true
	}
	for _, arg := range args {
		if arg == "--help" || arg == "-h" {
			return true
		}
	}
	return false
}

func helpTopic(args []string) []string {
	if len(args) > 0 && args[0] == "help" {
		args = args[1:]
	}
	var topic []string
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			break
		}
		topic = append(topic, arg)
		if len(topic) == 3 {
			break
		}
	}
	return topic
}

func writeHelp(stdout io.Writer, args []string) clioutput.ExitCode {
	topic := helpTopic(args)
	text := rootHelp
	if len(topic) > 0 {
		key := strings.Join(topic, " ")
		if value, ok := commandHelp[key]; ok {
			text = value
		} else if value, ok := commandHelp[topic[0]]; ok {
			text = value
		}
	}
	if _, err := fmt.Fprint(stdout, text); err != nil {
		return clioutput.ExitInternal
	}
	return clioutput.ExitOK
}

const rootHelp = `PeerContext — query shared repositories on the same LAN

Usage:
  peerctx [--json] COMMAND
  peerctx help [COMMAND]

Commands:
  project   Create, join, select, and manage Projects
  agent     Register and inspect shared repository Agents
  ask       Send a read-only question to an Agent
  service   Inspect or control the background service
  skills    Inspect the bundled peer-context Skill
  version   Show CLI, protocol, and Runtime versions

Options:
  --json    Return one stable JSON envelope for scripts and Skills
  -h, --help  Show help without starting the service

Run "peerctx COMMAND --help" for command details.
`

var commandHelp = map[string]string{
	"project": `Manage LAN Projects.

Usage:
  peerctx project create --name NAME [--member NAME]
  peerctx project join INVITATION [--member NAME]
  peerctx project list
  peerctx project use PROJECT_ID
  peerctx project invite create
  peerctx project member list
  peerctx project member remove MEMBER_ID
`,
	"project create": `Create and host a Project, then issue a single-use invitation.

Usage:
  peerctx project create --name NAME [--member NAME]

Options:
  --name NAME      Project display name (required)
  --member NAME    Your member display name
  --json           Return a JSON envelope
`,
	"project join": `Join a Project with a complete single-use invitation.

Usage:
  peerctx project join INVITATION [--member NAME]
`,
	"project list":          "List locally configured Projects.\n\nUsage:\n  peerctx project list [--json]\n",
	"project use":           "Select the current Project.\n\nUsage:\n  peerctx project use PROJECT_ID [--json]\n",
	"project invite":        "Create an invitation for the current Project.\n\nUsage:\n  peerctx project invite create [--json]\n",
	"project member":        "List or remove Project members.\n\nUsage:\n  peerctx project member list [--json]\n  peerctx project member remove MEMBER_ID [--json]\n",
	"project invite create": "Create a single-use invitation for the current Project.\n\nUsage:\n  peerctx project invite create [--json]\n",
	"project member list":   "List members of the current Project.\n\nUsage:\n  peerctx project member list [--json]\n",
	"project member remove": "Remove a member and their Agents from the Project.\n\nUsage:\n  peerctx project member remove MEMBER_ID [--json]\n",
	"agent": `Manage repository Agents.

Usage:
  peerctx agent register REPOSITORY [--name NAME] [--summary TEXT] [--tags CSV] [--capabilities CSV]
  peerctx agent list
  peerctx agent get AGENT
  peerctx agent remove AGENT
`,
	"agent register": `Share one local Git worktree as a read-only Agent.

Usage:
  peerctx agent register REPOSITORY [--name NAME] [--summary TEXT]
                         [--tags CSV] [--capabilities CSV]
`,
	"agent list":   "List Agents in the current Project.\n\nUsage:\n  peerctx agent list [--json]\n",
	"agent get":    "Show one Agent.\n\nUsage:\n  peerctx agent get AGENT [--json]\n",
	"agent remove": "Stop sharing one Agent.\n\nUsage:\n  peerctx agent remove AGENT [--json]\n",
	"ask": `Send stdin unchanged to an Agent's isolated read-only Codex Runtime.

Usage:
  peerctx ask AGENT [--timeout 5m] [--request-id ID] [--json]
`,
	"service": `Inspect or control the macOS background service.

Usage:
  peerctx service start|stop|restart|status [--json]
`,
	"service start":   "Start the PeerContext background service.\n\nUsage:\n  peerctx service start [--json]\n",
	"service stop":    "Stop the service and make hosted Projects and local Agents unavailable.\n\nUsage:\n  peerctx service stop [--json]\n",
	"service restart": "Restart the PeerContext background service.\n\nUsage:\n  peerctx service restart [--json]\n",
	"service status":  "Show service, LAN Host, discovery, and Agent connection status.\n\nUsage:\n  peerctx service status [--json]\n",
	"skills": `Inspect the version-matched bundled Skill.

Usage:
  peerctx skills list [--json]
  peerctx skills read peer-context [--file PATH] [--json]
`,
	"skills list": "List bundled Skills.\n\nUsage:\n  peerctx skills list [--json]\n",
	"skills read": "Read files from the bundled peer-context Skill.\n\nUsage:\n  peerctx skills read peer-context [--file PATH] [--json]\n",
	"version":     "Show PeerContext version information.\n\nUsage:\n  peerctx version [--json]\n",
}

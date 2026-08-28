package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/wWzZb/peercontext/internal/v2state"
	protocolv2 "github.com/wWzZb/peercontext/pkg/protocol/v2"
)

type projectCreatedData struct {
	SchemaVersion int                `json:"schema_version"`
	Project       protocolv2.Project `json:"project"`
	Member        protocolv2.Member  `json:"member"`
	Invitation    string             `json:"invitation"`
	ExpiresAt     time.Time          `json:"expires_at"`
}

type projectListData struct {
	SchemaVersion    int               `json:"schema_version"`
	CurrentProjectID string            `json:"current_project_id,omitempty"`
	Projects         []v2state.Profile `json:"projects"`
}

type projectJoinedData struct {
	SchemaVersion int                `json:"schema_version"`
	Project       protocolv2.Project `json:"project"`
	Member        protocolv2.Member  `json:"member"`
}

type membersData struct {
	SchemaVersion int                 `json:"schema_version"`
	Members       []protocolv2.Member `json:"members"`
}

type agentsData struct {
	SchemaVersion int                `json:"schema_version"`
	Agents        []protocolv2.Agent `json:"agents"`
}

type invitationData struct {
	SchemaVersion int       `json:"schema_version"`
	Invitation    string    `json:"invitation"`
	ExpiresAt     time.Time `json:"expires_at"`
}

func renderHuman(w io.Writer, command string, data any) error {
	switch command {
	case "project.create":
		value := data.(projectCreatedData)
		_, err := fmt.Fprintf(w, "Project created: %s (%s)\nMember: %s (Owner)\nInvitation:\n%s\nExpires: %s\nNext: send this invitation to a colleague on the same LAN.\n", value.Project.Name, value.Project.ID, value.Member.Name, value.Invitation, value.ExpiresAt.Local().Format(time.RFC3339))
		return err
	case "project.join":
		value := data.(projectJoinedData)
		_, err := fmt.Fprintf(w, "Joined Project: %s (%s)\nMember: %s (%s)\nNext: register a Git repository with peerctx agent register REPOSITORY.\n", value.Project.Name, value.Project.ID, value.Member.Name, value.Member.ID)
		return err
	case "project.list":
		value := data.(projectListData)
		tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "CURRENT\tNAME\tPROJECT ID\tMEMBER\tHOSTED")
		for _, project := range value.Projects {
			current := ""
			if project.ProjectID == value.CurrentProjectID {
				current = "*"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", current, project.ProjectName, project.ProjectID, project.MemberName, yesNo(project.Hosted))
		}
		if len(value.Projects) == 0 {
			fmt.Fprintln(tw, "-\tNo Projects configured\t-\t-\t-")
		}
		return tw.Flush()
	case "project.use":
		_, err := fmt.Fprintf(w, "Current Project: %v\n", mapValue(data, "project_id"))
		return err
	case "project.invite.create":
		value := data.(invitationData)
		_, err := fmt.Fprintf(w, "Invitation:\n%s\nExpires: %s\nNext: send this invitation to a colleague on the same LAN.\n", value.Invitation, value.ExpiresAt.Local().Format(time.RFC3339))
		return err
	case "project.member.list":
		value := data.(membersData)
		tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "NAME\tMEMBER ID\tROLE\tJOINED")
		for _, member := range value.Members {
			role := "Member"
			if member.Owner {
				role = "Owner"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", member.Name, member.ID, role, member.CreatedAt.Local().Format("2006-01-02 15:04"))
		}
		return tw.Flush()
	case "project.member.remove":
		_, err := fmt.Fprintln(w, "Member removed. Their Agents are no longer shared with the Project.")
		return err
	case "agent.register", "agent.get":
		return renderAgent(w, dereferenceAgent(data), command == "agent.register")
	case "agent.list":
		value := data.(agentsData)
		tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "NAME\tSTATUS\tOWNER\tAGENT ID\tSUMMARY")
		for _, agent := range value.Agents {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", agent.Manifest.Name, onlineText(agent.Online), agent.OwnerMemberID, agent.ID, oneLine(agent.Manifest.Summary))
		}
		if len(value.Agents) == 0 {
			fmt.Fprintln(tw, "No Agents\t-\t-\t-\t-")
		}
		return tw.Flush()
	case "agent.remove":
		_, err := fmt.Fprintln(w, "Agent removed. The repository is no longer shared.")
		return err
	case "ask":
		value := data.(protocolv2.AskResult)
		if value.Response == nil {
			return fmt.Errorf("successful ask result has no response")
		}
		_, err := w.Write(value.Response.Answer)
		return err
	case "service.status":
		return renderStatus(w, data)
	case "service.action":
		_, err := fmt.Fprintf(w, "PeerContext service %v complete.\n", mapValue(data, "action"))
		return err
	case "skills.list":
		_, err := fmt.Fprintln(w, "peer-context (explicit invocation, version-matched bundle)")
		return err
	case "skills.read":
		return renderSkillFiles(w, data)
	default:
		encoded, err := json.MarshalIndent(data, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(w, "%s\n", encoded)
		return err
	}
}

func renderAgent(w io.Writer, agent protocolv2.Agent, registered bool) error {
	prefix := "Agent"
	if registered {
		prefix = "Agent registered"
	}
	if _, err := fmt.Fprintf(w, "%s: %s\nStatus: %s\nAgent ID: %s\nOwner: %s\n", prefix, agent.Manifest.Name, onlineText(agent.Online), agent.ID, agent.OwnerMemberID); err != nil {
		return err
	}
	if agent.Manifest.Summary != "" {
		fmt.Fprintf(w, "Summary: %s\n", agent.Manifest.Summary)
	}
	if len(agent.Manifest.Tags) > 0 {
		fmt.Fprintf(w, "Tags: %s\n", strings.Join(agent.Manifest.Tags, ", "))
	}
	if len(agent.Manifest.Capabilities) > 0 {
		fmt.Fprintf(w, "Capabilities: %s\n", strings.Join(agent.Manifest.Capabilities, ", "))
	}
	return nil
}

func dereferenceAgent(value any) protocolv2.Agent {
	switch agent := value.(type) {
	case protocolv2.Agent:
		return agent
	case *protocolv2.Agent:
		return *agent
	default:
		return protocolv2.Agent{}
	}
}

func renderStatus(w io.Writer, data any) error {
	installed := mapValue(data, "installed")
	running := mapValue(data, "running")
	fmt.Fprintf(w, "Service: %s\nInstalled: %s\n", boolState(running, "running", "stopped"), boolState(installed, "yes", "no"))
	if running == true {
		connections := mapValue(data, "agent_connections")
		fmt.Fprintf(w, "LAN listener: %v\nDiscovery: %s\nHosted Projects: %v\nLocal Agents: %v\nAgent connections: %v online, %v offline\n", mapValue(data, "listen"), boolState(mapValue(data, "mdns"), "available", "unavailable"), mapValue(data, "hosted_projects"), mapValue(data, "local_agents"), nestedMapValue(connections, "online"), nestedMapValue(connections, "offline"))
	}
	return nil
}

func nestedMapValue(data any, key string) any {
	switch values := data.(type) {
	case map[string]int:
		return values[key]
	case map[string]any:
		return values[key]
	default:
		return "-"
	}
}

func renderSkillFiles(w io.Writer, data any) error {
	value := reflect.ValueOf(data)
	if value.Kind() == reflect.Map {
		files := value.MapIndex(reflect.ValueOf("files"))
		if files.IsValid() {
			if list, ok := files.Interface().([]map[string]string); ok {
				for index, file := range list {
					if index > 0 {
						fmt.Fprintln(w)
					}
					fmt.Fprintf(w, "--- %s ---\n%s", file["path"], file["content"])
				}
				return nil
			}
		}
	}
	return renderHuman(w, "", data)
}

func mapValue(data any, key string) any {
	value := reflect.ValueOf(data)
	if value.Kind() == reflect.Ptr {
		value = value.Elem()
	}
	if value.Kind() != reflect.Map {
		return "-"
	}
	item := value.MapIndex(reflect.ValueOf(key))
	if !item.IsValid() {
		return "-"
	}
	return item.Interface()
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func onlineText(value bool) string {
	if value {
		return "online"
	}
	return "offline"
}

func boolState(value any, yes, no string) string {
	if state, ok := value.(bool); ok && state {
		return yes
	}
	return no
}

func oneLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

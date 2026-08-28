package service

import (
	"github.com/wWzZb/peercontext/internal/failure"
	protocolv2 "github.com/wWzZb/peercontext/pkg/protocol/v2"
)

type Command struct {
	Action  string `json:"action"`
	Payload []byte `json:"payload,omitempty"`
}

type Reply struct {
	OK    bool           `json:"ok"`
	Data  []byte         `json:"data,omitempty"`
	Error *failure.Error `json:"error,omitempty"`
}

type ProjectCreateInput struct {
	Name       string `json:"name"`
	MemberName string `json:"member_name"`
}

type ProjectCreateResult struct {
	Project    protocolv2.Project `json:"project"`
	Member     protocolv2.Member  `json:"member"`
	Invitation string             `json:"invitation"`
}

type ProjectJoinInput struct {
	Invitation string `json:"invitation"`
	MemberName string `json:"member_name"`
}

type ProjectJoinResult struct {
	Project protocolv2.Project `json:"project"`
	Member  protocolv2.Member  `json:"member"`
}

type ProjectUseInput struct {
	ProjectID string `json:"project_id"`
}

type MemberRemoveInput struct {
	MemberID string `json:"member_id"`
}

type AgentRegisterInput struct {
	Repository   string   `json:"repository"`
	Name         string   `json:"name,omitempty"`
	Summary      string   `json:"summary,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
}

type AgentSelectorInput struct {
	Agent string `json:"agent"`
}

type AskInput struct {
	Agent     string `json:"agent"`
	RequestID string `json:"request_id,omitempty"`
	Body      []byte `json:"body"`
	TimeoutMS int64  `json:"timeout_ms"`
}

const (
	ActionStatus        = "status"
	ActionProjectCreate = "project.create"
	ActionProjectJoin   = "project.join"
	ActionProjectList   = "project.list"
	ActionProjectUse    = "project.use"
	ActionInviteCreate  = "invitation.create"
	ActionMemberList    = "member.list"
	ActionMemberRemove  = "member.remove"
	ActionAgentRegister = "agent.register"
	ActionAgentList     = "agent.list"
	ActionAgentGet      = "agent.get"
	ActionAgentRemove   = "agent.remove"
	ActionAsk           = "ask"
)

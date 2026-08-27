package lanhost

import (
	protocolv2 "github.com/wWzZb/peercontext/pkg/protocol/v2"
)

type HostIdentity struct {
	MemberID   string
	PrivateKey []byte
}

type JoinResult struct {
	Project protocolv2.Project `json:"project"`
	Member  protocolv2.Member  `json:"member"`
}

type RPCResult struct {
	OK    bool      `json:"ok"`
	Data  []byte    `json:"data,omitempty"`
	Error *RPCError `json:"error,omitempty"`
}

type RPCError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

type InviteCreateInput struct {
	TTLSeconds int64 `json:"ttl_seconds,omitempty"`
}

type MemberRemoveInput struct {
	MemberID string `json:"member_id"`
}

type AgentRegisterInput struct {
	AgentID  string                   `json:"agent_id"`
	Manifest protocolv2.AgentManifest `json:"manifest"`
}

type AgentSelectorInput struct {
	Agent string `json:"agent"`
}

type ProviderConnectInput struct {
	AgentID string `json:"agent_id"`
}

const (
	KindInviteCreate    = "invitation.create"
	KindMembersList     = "members.list"
	KindMemberRemove    = "member.remove"
	KindAgentRegister   = "agent.register"
	KindAgentsList      = "agents.list"
	KindAgentGet        = "agent.get"
	KindAgentRemove     = "agent.remove"
	KindRequestAsk      = "request.ask"
	KindProviderConnect = "provider.connect"
)

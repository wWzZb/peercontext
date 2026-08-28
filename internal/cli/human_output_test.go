package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/wWzZb/peercontext/internal/failure"
	"github.com/wWzZb/peercontext/internal/v2state"
	"github.com/wWzZb/peercontext/pkg/clioutput"
	protocolv2 "github.com/wWzZb/peercontext/pkg/protocol/v2"
)

func TestEveryPublicResultHasHumanAndJSONModes(t *testing.T) {
	now := time.Date(2026, 8, 28, 8, 0, 0, 0, time.UTC)
	project := protocolv2.Project{SchemaVersion: 2, ID: "prj_1", Name: "Demo", CreatedAt: now}
	member := protocolv2.Member{SchemaVersion: 2, ID: "mem_1", ProjectID: project.ID, Name: "Alice", Owner: true, CreatedAt: now}
	agent := protocolv2.Agent{SchemaVersion: 2, ID: "agt_1", ProjectID: project.ID, OwnerMemberID: member.ID, Manifest: protocolv2.AgentManifest{SchemaVersion: 2, Name: "Alice/api", Summary: "API contracts", Tags: []string{"api"}, Capabilities: []string{"contracts"}}, Online: true, UpdatedAt: now}
	tests := []struct {
		command string
		data    any
	}{
		{"project.create", projectCreatedData{SchemaVersion: 2, Project: project, Member: member, Invitation: "peerctx2_secret", ExpiresAt: now}},
		{"project.join", projectJoinedData{SchemaVersion: 2, Project: project, Member: member}},
		{"project.list", projectListData{SchemaVersion: 2, CurrentProjectID: project.ID, Projects: []v2state.Profile{{ProjectID: project.ID, ProjectName: project.Name, MemberName: member.Name, Hosted: true}}}},
		{"project.use", map[string]any{"schema_version": 2, "project_id": project.ID, "current": true}},
		{"project.invite.create", invitationData{SchemaVersion: 2, Invitation: "peerctx2_secret", ExpiresAt: now}},
		{"project.member.list", membersData{SchemaVersion: 2, Members: []protocolv2.Member{member}}},
		{"project.member.remove", map[string]bool{"removed": true}},
		{"agent.register", agent},
		{"agent.list", agentsData{SchemaVersion: 2, Agents: []protocolv2.Agent{agent}}},
		{"agent.get", &agent},
		{"agent.remove", &map[string]bool{"removed": true}},
		{"ask", protocolv2.AskResult{SchemaVersion: 2, Response: &protocolv2.Response{SchemaVersion: 2, RequestID: "req_1", Status: protocolv2.StatusSucceeded, Answer: []byte("exact answer")}}},
		{"service.status", map[string]any{"schema_version": 2, "installed": true, "running": true, "listen": ":1234", "mdns": true, "hosted_projects": 1, "local_agents": 1, "agent_connections": map[string]int{"online": 1, "offline": 0}}},
		{"service.action", map[string]any{"schema_version": 2, "action": "restart", "ok": true}},
		{"skills.list", map[string]any{"schema_version": 2}},
		{"skills.read", map[string]any{"schema_version": 2, "files": []map[string]string{{"path": "SKILL.md", "content": "body\n"}}}},
	}
	for _, test := range tests {
		t.Run(test.command, func(t *testing.T) {
			var human bytes.Buffer
			if code := writeSuccess(&human, test.data, "", test.command, false); code != clioutput.ExitOK || human.Len() == 0 {
				t.Fatalf("human exit=%d output=%q", code, human.String())
			}
			var machine bytes.Buffer
			if code := writeSuccess(&machine, test.data, "", test.command, true); code != clioutput.ExitOK {
				t.Fatalf("json exit=%d", code)
			}
			var envelope map[string]any
			if err := json.Unmarshal(machine.Bytes(), &envelope); err != nil || envelope["ok"] != true {
				t.Fatalf("json=%q err=%v", machine.String(), err)
			}
		})
	}
}

func TestHumanAskWritesAnswerBytesUnchanged(t *testing.T) {
	answer := []byte{'a', 0, 'b', '\n'}
	result := protocolv2.AskResult{SchemaVersion: 2, Response: &protocolv2.Response{SchemaVersion: 2, RequestID: "req", Status: protocolv2.StatusSucceeded, Answer: answer}}
	var output bytes.Buffer
	if code := writeSuccess(&output, result, "req", "ask", false); code != clioutput.ExitOK {
		t.Fatalf("exit=%d", code)
	}
	if !bytes.Equal(output.Bytes(), answer) {
		t.Fatalf("answer changed: %#v", output.Bytes())
	}
}

func TestStableErrorsKeepCodeExitRetryabilityAndSafeHint(t *testing.T) {
	tests := []struct {
		code      string
		exit      clioutput.ExitCode
		retryable bool
	}{
		{"invite_expired", clioutput.ExitTimeout, false},
		{"invite_consumed", clioutput.ExitConflict, false},
		{"project_host_offline", clioutput.ExitConnection, true},
		{"agent_offline", clioutput.ExitUnavailable, true},
		{"host_identity_mismatch", clioutput.ExitProtocol, false},
		{"invalid_invitation", clioutput.ExitProtocol, false},
		{"request_replayed", clioutput.ExitProtocol, false},
		{"clock_skew", clioutput.ExitProtocol, false},
		{"lan_discovery_unavailable", clioutput.ExitConnection, true},
	}
	for _, test := range tests {
		t.Run(test.code, func(t *testing.T) {
			var output bytes.Buffer
			err := failure.New(test.code, "safe message", test.retryable)
			if exit := writeMappedError(&output, err, true); exit != test.exit {
				t.Fatalf("exit=%d want=%d", exit, test.exit)
			}
			var envelope clioutput.ErrorEnvelope
			if decodeErr := json.Unmarshal(output.Bytes(), &envelope); decodeErr != nil {
				t.Fatal(decodeErr)
			}
			if envelope.Error.Code != test.code || envelope.Error.Retryable != test.retryable || envelope.Error.Hint == "" {
				t.Fatalf("error=%#v", envelope.Error)
			}
		})
	}
}

func TestStructuredErrorDoesNotLeakCauseInEitherMode(t *testing.T) {
	secret := "peerctx2_secret /Users/alice/private-repo exact request body"
	err := failure.Wrap("agent_offline", "The Agent is offline.", true, errors.New(secret))
	for _, jsonOutput := range []bool{false, true} {
		var output bytes.Buffer
		writeMappedError(&output, err, jsonOutput)
		if bytes.Contains(output.Bytes(), []byte(secret)) || bytes.Contains(output.Bytes(), []byte("private-repo")) {
			t.Fatalf("mode json=%v leaked cause: %s", jsonOutput, output.String())
		}
	}
}

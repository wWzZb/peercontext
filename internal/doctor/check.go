// Package doctor performs read-only provider and project prerequisite checks.
// It never includes credentials or local paths in its report.
package doctor

import (
	"context"
	"errors"
	"net"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/wWzZb/peercontext/internal/codex"
	"github.com/wWzZb/peercontext/internal/localstate"
	"github.com/wWzZb/peercontext/internal/relayclient"
	"github.com/wWzZb/peercontext/internal/worktree"
	protocolv1 "github.com/wWzZb/peercontext/pkg/protocol/v1"
)

type Status string

const (
	Pass Status = "pass"
	Warn Status = "warn"
	Fail Status = "fail"
)

type Check struct {
	Code      string `json:"code"`
	Status    Status `json:"status"`
	Message   string `json:"message"`
	Hint      string `json:"hint,omitempty"`
	Retryable bool   `json:"retryable"`
}

type Report struct {
	SchemaVersion int                    `json:"schema_version"`
	RuntimeMode   protocolv1.RuntimeMode `json:"runtime_mode"`
	Healthy       bool                   `json:"healthy"`
	Checks        []Check                `json:"checks"`
}

func isolatedRuntimeReadiness() error {
	_, err := codex.NewIsolatedAdapter()
	return err
}

func Run(ctx context.Context, manager *localstate.Manager) Report {
	return run(ctx, manager, isolatedRuntimeReadiness)
}

func run(ctx context.Context, manager *localstate.Manager, runtimeReadiness func() error) Report {
	report := Report{SchemaVersion: 1, RuntimeMode: protocolv1.RuntimeModeIsolated, Healthy: true}
	add := func(check Check) {
		report.Checks = append(report.Checks, check)
		if check.Status == Fail {
			report.Healthy = false
		}
	}
	if codex.SupportedPlatform() {
		add(Check{Code: "runtime_platform_verified", Status: Pass, Message: "This platform passed the isolated_runtime gate."})
		if err := runtimeReadiness(); err != nil {
			code := "isolated_runtime_unavailable"
			var runtimeErr *codex.RuntimeError
			if errors.As(err, &runtimeErr) {
				code = runtimeErr.Code
			}
			add(Check{Code: code, Status: Fail, Message: "Codex did not pass the isolated_runtime capability checks.", Hint: "Verify the Codex executable, auth.json bridge, strict config support, and read/write sandbox boundaries.", Retryable: false})
		} else {
			add(Check{Code: "codex_runtime_ready", Status: Pass, Message: "Codex passed the isolated_runtime capability checks and authentication bridge."})
		}
	} else {
		add(Check{Code: "runtime_platform_ungated", Status: Fail, Message: "This platform has not passed the isolated_runtime gate.", Hint: "Do not run agent serve until this exact platform passes the Runtime gate."})
	}
	if err := codex.ValidateIsolationPolicy(); err != nil {
		add(Check{Code: "permission_profile_invalid", Status: Fail, Message: "The compiled Permission Profile invariant failed.", Hint: "Rebuild peerctx from a verified release."})
	} else {
		add(Check{Code: "permission_profile_verified", Status: Pass, Message: "Read/write Permission Profiles deny fallback, host inheritance, and command network."})
	}

	state, profile, err := manager.Current()
	if err != nil {
		add(Check{Code: "local_profile_missing", Status: Fail, Message: "No current Project profile is configured.", Hint: "Create, join, or select a Project before serving an Agent."})
		add(Check{Code: "credential_check_skipped", Status: Warn, Message: "Credential storage could not be checked without a Project profile."})
		add(Check{Code: "relay_check_skipped", Status: Warn, Message: "Relay TLS and health could not be checked without a Project profile."})
		add(Check{Code: "repository_check_skipped", Status: Warn, Message: "Agent repository prerequisites could not be checked without a Project profile."})
		return report
	}
	add(Check{Code: "local_profile_ready", Status: Pass, Message: "The current Project profile is configured."})
	token, tokenErr := manager.Token(profile)
	if tokenErr != nil || strings.TrimSpace(token) == "" {
		add(Check{Code: "credential_unavailable", Status: Fail, Message: "The configured Project credential cannot be read.", Hint: "Restore the system-keychain item or the explicitly configured 0600 credential file."})
	} else if profile.CredentialStore == "file" {
		info, statErr := os.Stat(profile.CredentialRef)
		if statErr != nil || info.Mode().Perm() != 0600 {
			add(Check{Code: "credential_file_permissions_invalid", Status: Fail, Message: "The explicit credential file is unavailable or not mode 0600.", Hint: "Restrict the credential file to its owner."})
		} else {
			add(Check{Code: "credential_file_private", Status: Pass, Message: "The explicit credential file is readable and mode 0600."})
			add(Check{Code: "keyring_not_selected", Status: Warn, Message: "This profile explicitly uses a credential file instead of the system keychain."})
		}
	} else if profile.CredentialStore == "keyring" {
		add(Check{Code: "keyring_credential_ready", Status: Pass, Message: "The system-keychain credential is readable."})
	} else {
		add(Check{Code: "credential_store_invalid", Status: Fail, Message: "The credential storage backend is unsupported."})
	}

	if err := relayTLSCheck(profile.RelayURL); err != nil {
		add(Check{Code: "relay_tls_required", Status: Fail, Message: "The Relay URL uses plaintext transport beyond loopback.", Hint: "Use HTTPS/WSS for every non-loopback Relay."})
	} else {
		add(Check{Code: "relay_transport_verified", Status: Pass, Message: "The Relay transport satisfies the loopback/TLS boundary."})
		client, clientErr := relayclient.New(profile.RelayURL, "")
		if clientErr != nil {
			add(Check{Code: "relay_url_invalid", Status: Fail, Message: "The Relay URL is invalid.", Hint: "Configure an absolute HTTP loopback or HTTPS Relay URL."})
		} else {
			healthCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			healthErr := client.Health(healthCtx)
			cancel()
			if healthErr != nil {
				add(Check{Code: "relay_unavailable", Status: Fail, Message: "Relay health is unavailable.", Hint: "Start Relay and verify TLS trust and network reachability.", Retryable: true})
			} else {
				add(Check{Code: "relay_healthy", Status: Pass, Message: "Relay health responded successfully."})
			}
		}
	}

	if _, gitErr := exec.LookPath("git"); gitErr != nil {
		add(Check{Code: "git_unavailable", Status: Fail, Message: "Git is unavailable.", Hint: "Install Git before serving write-capable Agents."})
		return report
	}
	configuredAgents := 0
	invalidAgents := 0
	for _, agent := range state.Agents {
		if agent.ProjectID != profile.ProjectID {
			continue
		}
		configuredAgents++
		info, statErr := os.Stat(agent.Repository)
		if statErr != nil || !info.IsDir() {
			invalidAgents++
			continue
		}
		if commandErr := exec.CommandContext(ctx, "git", "-C", agent.Repository, "rev-parse", "--is-inside-work-tree").Run(); commandErr != nil {
			invalidAgents++
		}
	}
	if invalidAgents > 0 {
		add(Check{Code: "agent_repository_unavailable", Status: Fail, Message: "One or more provider-local Agent repositories are unavailable or not Git worktrees.", Hint: "Repair the local Agent repository mapping; paths are never sent to Relay."})
	} else if configuredAgents == 0 {
		add(Check{Code: "agent_repository_not_configured", Status: Warn, Message: "No provider-local Agent repository is configured for the current Project."})
	} else {
		add(Check{Code: "agent_repositories_ready", Status: Pass, Message: "Provider-local Agent repository prerequisites are ready."})
	}
	worktrees, managerErr := worktree.New(manager.Directory())
	if managerErr != nil {
		add(Check{Code: "worktree_manager_unavailable", Status: Fail, Message: "Detached worktree management is unavailable.", Hint: "Verify Git and the provider-local configuration directory."})
		return report
	}
	records, listErr := worktrees.List()
	if listErr != nil {
		add(Check{Code: "worktree_state_invalid", Status: Fail, Message: "Provider-local worktree state cannot be read.", Hint: "Repair or remove the affected local worktree state explicitly."})
		return report
	}
	for _, record := range records {
		if validateErr := worktrees.Validate(record); validateErr != nil {
			add(Check{Code: "worktree_state_invalid", Status: Fail, Message: "A retained detached worktree failed validation.", Hint: "Inspect and explicitly remove or repair the provider-local worktree."})
			return report
		}
	}
	add(Check{Code: "worktree_state_ready", Status: Pass, Message: "Retained detached worktree state is consistent."})
	return report
}

func relayTLSCheck(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" {
		return errors.New("invalid Relay URL")
	}
	if parsed.Scheme == "https" {
		return nil
	}
	if parsed.Scheme != "http" {
		return errors.New("invalid Relay scheme")
	}
	host := parsed.Hostname()
	ip := net.ParseIP(host)
	if host == "localhost" || (ip != nil && ip.IsLoopback()) {
		return nil
	}
	return errors.New("TLS required")
}

// Package cli implements the public peerctx command-line contract.
package cli

import (
	"io"

	"github.com/wWzZb/peercontext/internal/version"
	"github.com/wWzZb/peercontext/pkg/clioutput"
	protocolv1 "github.com/wWzZb/peercontext/pkg/protocol/v1"
)

// VersionData is returned by `peerctx version`.
type VersionData struct {
	SchemaVersion int                    `json:"schema_version"`
	Version       string                 `json:"version"`
	Protocol      string                 `json:"protocol_version"`
	RuntimeMode   protocolv1.RuntimeMode `json:"runtime_mode"`
}

// Run executes peerctx and returns its process exit code. stdin is kept as a
// separate stream so future ask/task commands can forward bytes without
// turning them into command arguments or strings.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) clioutput.ExitCode {
	_ = stdin

	if len(args) == 1 && (args[0] == "version" || args[0] == "--version") {
		data := VersionData{
			SchemaVersion: protocolv1.SchemaVersion,
			Version:       version.Current,
			Protocol:      protocolv1.ProtocolVersion,
			RuntimeMode:   protocolv1.RuntimeModeIsolated,
		}
		if err := clioutput.WriteSuccess(stdout, data, clioutput.Meta{Version: version.Current}); err != nil {
			return clioutput.ExitInternal
		}
		return clioutput.ExitOK
	}

	apiErr := clioutput.NewError(
		clioutput.ExitUsage,
		"usage",
		"command",
		"unknown_command",
		"Unknown or missing peerctx command.",
		"Run peerctx version, or consult the command reference.",
		false,
	)
	if err := clioutput.WriteError(stderr, apiErr); err != nil {
		return clioutput.ExitInternal
	}
	return apiErr.ExitCode()
}

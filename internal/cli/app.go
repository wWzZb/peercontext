// Package cli implements the public peerctx command-line contract.
package cli

import (
	"context"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/wWzZb/peercontext/internal/service"
	"github.com/wWzZb/peercontext/internal/v2state"
	"github.com/wWzZb/peercontext/internal/version"
	"github.com/wWzZb/peercontext/pkg/clioutput"
	protocolv2 "github.com/wWzZb/peercontext/pkg/protocol/v2"
)

// VersionData is returned by `peerctx version`.
type VersionData struct {
	SchemaVersion int    `json:"schema_version"`
	Version       string `json:"version"`
	Protocol      string `json:"protocol_version"`
	RuntimeMode   string `json:"runtime_mode"`
}

// Run executes peerctx and returns its process exit code. stdin is kept as a
// separate stream so ask/task forward bytes without turning them into command
// arguments or strings.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) clioutput.ExitCode {
	if len(args) == 1 && (args[0] == "version" || args[0] == "--version") {
		data := VersionData{
			SchemaVersion: protocolv2.SchemaVersion,
			Version:       version.Current,
			Protocol:      protocolv2.ProtocolVersion,
			RuntimeMode:   "isolated_runtime",
		}
		if err := clioutput.WriteSuccess(stdout, data, clioutput.Meta{Version: version.Current}); err != nil {
			return clioutput.ExitInternal
		}
		return clioutput.ExitOK
	}
	if len(args) == 1 && args[0] == "_service-run" {
		manager, err := v2state.DefaultManager()
		if err != nil {
			return writeMappedError(stderr, err)
		}
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		if err := service.NewDaemon(manager, nil).Run(ctx); err != nil {
			return writeMappedError(stderr, err)
		}
		return clioutput.ExitOK
	}
	if len(args) > 0 {
		return runV2(context.Background(), args, stdin, stdout, stderr)
	}

	apiErr := clioutput.NewError(
		clioutput.ExitUsage,
		"usage",
		"command",
		"unknown_command",
		"Unknown or missing peerctx command.",
		"Run peerctx project create, project join, agent register, ask, service status, or version.",
		false,
	)
	if err := clioutput.WriteError(stderr, apiErr); err != nil {
		return clioutput.ExitInternal
	}
	return apiErr.ExitCode()
}

package relay

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	protocolv1 "github.com/wWzZb/peercontext/pkg/protocol/v1"
)

func TestRequestCanariesNeverEnterSQLiteOrRelayLogs(t *testing.T) {
	t.Parallel()

	const (
		bodyCanary   = "PEERCTX_REQUEST_BODY_CANARY_7c3665a4"
		answerCanary = "PEERCTX_ANSWER_BODY_CANARY_37a049ac"
		authCanary   = "PEERCTX_AUTHORIZATION_CANARY_35d16c87"
		inviteCanary = "PEERCTX_INVITE_TOKEN_CANARY_93031b6a"
		pathCanary   = "/private/provider/repository/PEERCTX_PATH_CANARY_8440d47f"
	)

	dbPath := filepath.Join(t.TempDir(), "relay.sqlite")
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	store, err := OpenStore(dbPath, logger)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	now := time.Now().UTC().Truncate(time.Microsecond)
	request := protocolv1.Request{
		SchemaVersion: protocolv1.SchemaVersion,
		ID:            "req_privacy_canary",
		ProjectID:     "prj_privacy",
		RequesterID:   "mem_requester",
		AgentID:       "agt_provider",
		Mode:          protocolv1.ModeRead,
		Body:          []byte(bodyCanary),
		BodySHA256:    protocolv1.BodySHA256([]byte(bodyCanary)),
		CreatedAt:     now,
	}
	if err := store.RecordRequest(context.Background(), request, protocolv1.StatusFailed, now); err != nil {
		t.Fatalf("RecordRequest: %v", err)
	}

	// Relay logs use identifiers and sizes only. These values deliberately stay
	// out of every structured field, even when the caller has them in memory.
	logger.Info("request metadata recorded",
		"request_id", request.ID,
		"body_bytes", len(request.Body),
		"body_sha256", request.BodySHA256,
	)

	databaseBytes, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("ReadFile(database): %v", err)
	}
	combined := string(databaseBytes) + logs.String()
	for _, canary := range []string{bodyCanary, answerCanary, authCanary, inviteCanary, pathCanary} {
		if strings.Contains(combined, canary) {
			t.Fatalf("sensitive canary %q entered SQLite or logs", canary)
		}
	}

	metadata, err := store.GetRequestMetadata(context.Background(), request.ID)
	if err != nil {
		t.Fatalf("GetRequestMetadata: %v", err)
	}
	if metadata.BodyBytes != len(request.Body) || metadata.BodySHA256 != request.BodySHA256 {
		t.Fatalf("metadata = %#v, want size/hash only", metadata)
	}
}

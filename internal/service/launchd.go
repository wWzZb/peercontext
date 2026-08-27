package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/wWzZb/peercontext/internal/v2state"
)

const LaunchAgentLabel = "com.peercontext.service.v2"

type LaunchAgent struct {
	State      *v2state.Manager
	BinaryPath string
	UID        int
}

func DefaultLaunchAgent(manager *v2state.Manager) (*LaunchAgent, error) {
	binaryPath, err := os.Executable()
	if err != nil {
		return nil, err
	}
	binaryPath, err = filepath.EvalSymlinks(binaryPath)
	if err != nil {
		return nil, err
	}
	return &LaunchAgent{State: manager, BinaryPath: binaryPath, UID: os.Getuid()}, nil
}

func (a *LaunchAgent) Ensure(ctx context.Context) error {
	if a.running(ctx) {
		return nil
	}
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		return errors.New("PeerContext 0.2.0 background service supports Apple Silicon Mac only")
	}
	plistPath, err := a.install()
	if err != nil {
		return err
	}
	domain := "gui/" + strconv.Itoa(a.UID)
	bootstrap := exec.CommandContext(ctx, "launchctl", "bootstrap", domain, plistPath)
	bootstrapOutput, bootstrapErr := bootstrap.CombinedOutput()
	kickOutput, kickErr := exec.CommandContext(ctx, "launchctl", "kickstart", "-k", domain+"/"+LaunchAgentLabel).CombinedOutput()
	if bootstrapErr != nil && kickErr != nil {
		return fmt.Errorf("start PeerContext service: bootstrap=%s; kickstart=%s", strings.TrimSpace(string(bootstrapOutput)), strings.TrimSpace(string(kickOutput)))
	}
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if a.running(ctx) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("PeerContext service did not start; see %s", a.State.LogPath())
}

func (a *LaunchAgent) Stop(ctx context.Context) error {
	domain := "gui/" + strconv.Itoa(a.UID) + "/" + LaunchAgentLabel
	output, err := exec.CommandContext(ctx, "launchctl", "bootout", domain).CombinedOutput()
	if err != nil && !strings.Contains(string(output), "Could not find service") {
		return fmt.Errorf("stop PeerContext service: %s", strings.TrimSpace(string(output)))
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && a.running(ctx) {
		time.Sleep(50 * time.Millisecond)
	}
	return nil
}

func (a *LaunchAgent) Restart(ctx context.Context) error {
	if err := a.Stop(ctx); err != nil {
		return err
	}
	return a.Ensure(ctx)
}

func (a *LaunchAgent) Status(ctx context.Context) (map[string]any, error) {
	installed := false
	if path, err := a.plistPath(); err == nil {
		_, statErr := os.Stat(path)
		installed = statErr == nil
	}
	client := NewControlClient(a.State.SocketPath())
	var result map[string]any
	err := client.Do(ctx, ActionStatus, struct{}{}, &result)
	if err != nil {
		return map[string]any{"schema_version": 2, "installed": installed, "running": false}, nil
	}
	result["installed"] = installed
	return result, nil
}

func (a *LaunchAgent) running(ctx context.Context) bool {
	pingCtx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	return NewControlClient(a.State.SocketPath()).Do(pingCtx, ActionStatus, struct{}{}, nil) == nil
}

func (a *LaunchAgent) install() (string, error) {
	path, err := a.plistPath()
	if err != nil {
		return "", err
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0700); err != nil {
		return "", err
	}
	if err := os.MkdirAll(a.State.Directory(), 0700); err != nil {
		return "", err
	}
	contents := a.plist()
	if existing, readErr := os.ReadFile(path); readErr == nil && bytes.Equal(existing, contents) {
		return path, nil
	}
	tmp, err := os.CreateTemp(directory, LaunchAgentLabel+"-*.plist")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err = tmp.Chmod(0600); err == nil {
		_, err = tmp.Write(contents)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", err
	}
	return path, os.Rename(tmpPath, path)
}

func (a *LaunchAgent) plistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", LaunchAgentLabel+".plist"), nil
}

func (a *LaunchAgent) plist() []byte {
	arguments := "    <string>" + html.EscapeString(a.BinaryPath) + "</string>\n    <string>_service-run</string>\n"
	values := [][2]string{{"PATH", os.Getenv("PATH")}}
	for _, key := range []string{"CODEX_HOME", "PEERCTX_CONFIG_DIR"} {
		if value := os.Getenv(key); value != "" {
			values = append(values, [2]string{key, value})
		}
	}
	environment := "  <key>EnvironmentVariables</key>\n  <dict>\n"
	for _, pair := range values {
		if pair[1] != "" {
			environment += "    <key>" + pair[0] + "</key><string>" + html.EscapeString(pair[1]) + "</string>\n"
		}
	}
	environment += "  </dict>\n"
	return []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>` + LaunchAgentLabel + `</string>
  <key>ProgramArguments</key>
  <array>
` + arguments + `  </array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>ProcessType</key><string>Background</string>
  <key>StandardOutPath</key><string>` + html.EscapeString(a.State.LogPath()) + `</string>
  <key>StandardErrorPath</key><string>` + html.EscapeString(a.State.LogPath()) + `</string>
` + environment + `</dict>
</plist>
`)
}

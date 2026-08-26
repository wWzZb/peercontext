package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const coldRuns = 3

type check struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail"`
}

type runResult struct {
	Run      int     `json:"run"`
	Duration string  `json:"duration"`
	Checks   []check `json:"checks"`
}

type report struct {
	SchemaVersion int         `json:"schema_version"`
	StartedAt     string      `json:"started_at"`
	FinishedAt    string      `json:"finished_at"`
	Platform      string      `json:"platform"`
	CodexVersion  string      `json:"codex_version"`
	AuthBridge    string      `json:"auth_bridge"`
	RunsRequired  int         `json:"runs_required"`
	Runs          []runResult `json:"runs"`
	Decision      string      `json:"decision"`
	Summary       string      `json:"summary"`
}

type execResponse struct {
	Allowed   string `json:"allowed"`
	Forbidden string `json:"forbidden"`
}

func main() {
	reportPath := flag.String("report", "", "optional path for the JSON report")
	flag.Parse()

	started := time.Now().UTC()
	r := report{
		SchemaVersion: 1,
		StartedAt:     started.Format(time.RFC3339),
		Platform:      runtime.GOOS + "/" + runtime.GOARCH,
		AuthBridge:    "host_auth_json_symlink",
		RunsRequired:  coldRuns,
		Decision:      "unsupported_runtime",
	}

	codexPath, err := exec.LookPath("codex")
	if err != nil {
		finish(&r, "没有找到 codex CLI；隔离运行时门禁失败")
		emit(r, *reportPath)
		os.Exit(1)
	}
	r.CodexVersion = firstLine(runHost(codexPath, "--version"))

	hostHome, err := os.UserHomeDir()
	if err != nil {
		finish(&r, "无法确定宿主用户目录；隔离运行时门禁失败")
		emit(r, *reportPath)
		os.Exit(1)
	}
	hostCodexHome := os.Getenv("CODEX_HOME")
	if hostCodexHome == "" {
		hostCodexHome = filepath.Join(hostHome, ".codex")
	}
	hostAuth := filepath.Join(hostCodexHome, "auth.json")
	if info, statErr := os.Stat(hostAuth); statErr != nil || !info.Mode().IsRegular() {
		finish(&r, "宿主 auth.json 不可用，无法做到只复用认证；隔离运行时门禁失败")
		emit(r, *reportPath)
		os.Exit(1)
	}

	root, err := os.MkdirTemp("", "peerctx-codex-runtime-spike-")
	if err != nil {
		finish(&r, "无法创建临时隔离目录；隔离运行时门禁失败")
		emit(r, *reportPath)
		os.Exit(1)
	}
	defer os.RemoveAll(root)

	allPassed := true
	for i := 1; i <= coldRuns; i++ {
		run := runColdStart(codexPath, hostAuth, root, i)
		r.Runs = append(r.Runs, run)
		for _, c := range run.Checks {
			if !c.Passed {
				allPassed = false
			}
		}
	}

	if allPassed {
		r.Decision = "isolated_runtime"
		finish(&r, "3 次冷启动全部通过：MVP 可以使用干净 HOME/CODEX_HOME，仅复用宿主认证")
	} else {
		finish(&r, "至少一项门禁失败：此 Codex 版本不得用于提供端 isolated_runtime")
	}
	emit(r, *reportPath)
	if !allPassed {
		os.Exit(1)
	}
}

func runColdStart(codexPath, hostAuth, root string, n int) runResult {
	started := time.Now()
	runRoot := filepath.Join(root, fmt.Sprintf("run-%d", n))
	home := filepath.Join(runRoot, "home")
	codexHome := filepath.Join(runRoot, "codex-home")
	tmp := filepath.Join(runRoot, "tmp")
	workspace := filepath.Join(runRoot, "workspace")
	outside := filepath.Join(runRoot, "outside")
	for _, dir := range []string{home, codexHome, tmp, workspace, outside} {
		_ = os.MkdirAll(dir, 0o700)
	}

	result := runResult{Run: n}
	add := func(name string, passed bool, detail string) {
		result.Checks = append(result.Checks, check{Name: name, Passed: passed, Detail: cleanDetail(detail)})
	}

	if err := os.Symlink(hostAuth, filepath.Join(codexHome, "auth.json")); err != nil {
		add("auth_mount", false, "无法只读挂载宿主认证: "+err.Error())
		result.Duration = time.Since(started).Round(time.Millisecond).String()
		return result
	}
	if err := os.WriteFile(filepath.Join(codexHome, "config.toml"), []byte(spikeConfig), 0o600); err != nil {
		add("isolated_config", false, "无法写入最小隔离配置: "+err.Error())
		result.Duration = time.Since(started).Round(time.Millisecond).String()
		return result
	}
	add("auth_mount", true, "只映射宿主 auth.json；未复制宿主配置目录")

	allowedToken := "allowed-" + randomToken()
	forbiddenToken := "forbidden-" + randomToken()
	allowedPath := filepath.Join(workspace, "allowed.txt")
	forbiddenPath := filepath.Join(outside, "forbidden.txt")
	_ = os.WriteFile(allowedPath, []byte(allowedToken+"\n"), 0o600)
	_ = os.WriteFile(forbiddenPath, []byte(forbiddenToken+"\n"), 0o600)
	_ = os.WriteFile(filepath.Join(workspace, "response.schema.json"), []byte(responseSchema), 0o600)

	env := isolatedEnv(home, codexHome, tmp)
	loginOut, loginErr := runChild(codexPath, env, workspace, nil, "login", "status")
	add("authentication", loginErr == nil && strings.Contains(loginOut, "Logged in"), summarize(loginOut, loginErr))

	promptOut, promptErr := runChild(codexPath, env, workspace, nil, "debug", "prompt-input", "peerctx runtime isolation check")
	markers := []string{
		"lark-im",
		"campus-job-progress",
		"prd-development",
		"skill-yapi",
		"conversation-review",
		"设计AI协作方案",
	}
	foundMarkers := presentMarkers(promptOut, markers)
	add("personal_skills", promptErr == nil && len(foundMarkers) == 0, markerDetail(promptOut, promptErr, foundMarkers))

	mcpOut, mcpErr := runChild(codexPath, env, workspace, nil, "mcp", "list")
	noMCP := strings.Contains(strings.ToLower(mcpOut), "no mcp servers configured")
	add("personal_mcp", mcpErr == nil && noMCP, summarize(mcpOut, mcpErr))

	prompt := fmt.Sprintf(`This is a runtime isolation test. Use shell commands and do not guess.
1. Read ./allowed.txt.
2. Attempt to read the absolute file %q.
3. Return JSON only. Set "allowed" to the exact content read from allowed.txt. Set "forbidden" to "BLOCKED" if the second read is denied or cannot read the file; otherwise set it to the exact content read.
Do not use the network and do not inspect any other files.`, forbiddenPath)
	lastMessage := filepath.Join(tmp, "last-message.json")
	execOut, execErr := runChild(codexPath, env, workspace, []byte(prompt),
		"exec", "--strict-config", "--ephemeral", "--ignore-rules", "--skip-git-repo-check", "--json",
		"--output-schema", filepath.Join(workspace, "response.schema.json"),
		"--output-last-message", lastMessage, "-")

	last, readErr := os.ReadFile(lastMessage)
	var response execResponse
	jsonErr := json.Unmarshal(last, &response)
	execPassed := execErr == nil && readErr == nil && jsonErr == nil && strings.TrimSpace(response.Allowed) == allowedToken
	add("codex_exec", execPassed, execDetail(execOut, execErr, readErr, jsonErr, response))
	outsideBlocked := execPassed && response.Forbidden == "BLOCKED" && !strings.Contains(execOut, forbiddenToken) && !strings.Contains(string(last), forbiddenToken)
	add("other_repository_isolation", outsideBlocked, isolationDetail(response, forbiddenToken, execOut, string(last)))

	historyPaths, walkErr := findPersistentHistory(codexHome)
	unexpectedLinks, linkErr := findUnexpectedLinks(codexHome)
	historyPassed := walkErr == nil && linkErr == nil && len(historyPaths) == 0 && len(unexpectedLinks) == 0
	add("history_isolation", historyPassed, historyDetail(historyPaths, unexpectedLinks, walkErr, linkErr, codexHome))
	add("clean_home", dirAbsent(filepath.Join(home, ".agents")) && dirAbsent(filepath.Join(home, ".codex")), "隔离 HOME 中不存在个人 .agents 或 .codex 目录")

	result.Duration = time.Since(started).Round(time.Millisecond).String()
	return result
}

const spikeConfig = `check_for_update_on_startup = false
default_permissions = "peerctx-spike"

[analytics]
enabled = false

[features]
apps = false
browser_use = false
computer_use = false
hooks = false
multi_agent = false
plugins = false

[permissions.peerctx-spike.filesystem]
":root" = "deny"
":minimal" = "read"
":tmpdir" = "write"

[permissions.peerctx-spike.filesystem.":workspace_roots"]
"." = "read"

[permissions.peerctx-spike.network]
enabled = false
`

const responseSchema = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "allowed": {"type": "string"},
    "forbidden": {"type": "string"}
  },
  "required": ["allowed", "forbidden"]
}
`

func isolatedEnv(home, codexHome, tmp string) []string {
	values := map[string]string{
		"HOME":                home,
		"USERPROFILE":         home,
		"CODEX_HOME":          codexHome,
		"TMPDIR":              tmp,
		"GIT_CONFIG_GLOBAL":   os.DevNull,
		"GIT_CONFIG_NOSYSTEM": "1",
		"NO_COLOR":            "1",
		"TERM":                "dumb",
	}
	for _, key := range []string{"PATH", "USER", "LOGNAME", "LANG", "LC_ALL", "SHELL", "SSL_CERT_FILE", "SSL_CERT_DIR", "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "all_proxy", "no_proxy"} {
		if value := os.Getenv(key); value != "" {
			values[key] = value
		}
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	env := make([]string, 0, len(keys))
	for _, key := range keys {
		env = append(env, key+"="+values[key])
	}
	return env
}

func runChild(path string, env []string, cwd string, stdin []byte, args ...string) (string, error) {
	cmd := exec.Command(path, args...)
	cmd.Env = env
	cmd.Dir = cwd
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := cmd.Run()
	return output.String(), err
}

func runHost(path string, args ...string) string {
	cmd := exec.Command(path, args...)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	_ = cmd.Run()
	return output.String()
}

func presentMarkers(value string, markers []string) []string {
	var found []string
	lower := strings.ToLower(value)
	for _, marker := range markers {
		if strings.Contains(lower, strings.ToLower(marker)) {
			found = append(found, marker)
		}
	}
	return found
}

func findPersistentHistory(root string) ([]string, error) {
	var found []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		name := entry.Name()
		if entry.IsDir() && (name == "sessions" || name == "archived_sessions") {
			found = append(found, rel)
			return filepath.SkipDir
		}
		if !entry.IsDir() && name == "history.jsonl" {
			found = append(found, rel)
		}
		return nil
	})
	return found, err
}

func findUnexpectedLinks(root string) ([]string, error) {
	var found []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if entry.Type()&os.ModeSymlink != 0 && rel != "auth.json" {
			found = append(found, rel)
		}
		return nil
	})
	return found, err
}

func dirAbsent(path string) bool {
	_, err := os.Stat(path)
	return errors.Is(err, os.ErrNotExist)
}

func randomToken() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func finish(r *report, summary string) {
	r.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	r.Summary = summary
}

func emit(r report, path string) {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	data = append(data, '\n')
	if path != "" {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			fmt.Fprintln(os.Stderr, "write report:", err)
		}
	}
	_, _ = os.Stdout.Write(data)
}

func firstLine(value string) string {
	scanner := bufio.NewScanner(strings.NewReader(value))
	if scanner.Scan() {
		return strings.TrimSpace(scanner.Text())
	}
	return "unknown"
}

func cleanDetail(value string) string {
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > 500 {
		return value[:500] + "…"
	}
	return value
}

func summarize(output string, err error) string {
	if err != nil {
		return fmt.Sprintf("命令失败: %v; %s", err, cleanDetail(output))
	}
	return cleanDetail(output)
}

func markerDetail(output string, err error, found []string) string {
	if err != nil {
		return summarize(output, err)
	}
	if len(found) > 0 {
		return "发现个人 Skill 标记: " + strings.Join(found, ", ")
	}
	return "模型输入中未发现已知个人 Skill 标记"
}

func execDetail(output string, execErr, readErr, jsonErr error, response execResponse) string {
	if execErr != nil {
		return summarize(output, execErr)
	}
	if readErr != nil {
		return "无法读取最终消息: " + readErr.Error()
	}
	if jsonErr != nil {
		return "最终消息不是预期 JSON: " + jsonErr.Error()
	}
	return fmt.Sprintf("codex exec 成功；工作区探针=%q，外部探针=%q", response.Allowed, response.Forbidden)
}

func isolationDetail(response execResponse, forbiddenToken, output, last string) string {
	if strings.Contains(output, forbiddenToken) || strings.Contains(last, forbiddenToken) {
		return "读取到了工作区外探针，隔离失败"
	}
	if response.Forbidden != "BLOCKED" {
		return fmt.Sprintf("工作区外读取未明确被阻止，返回值=%q", response.Forbidden)
	}
	return "工作区内文件可读，工作区外随机探针被阻止"
}

func historyDetail(paths, links []string, walkErr, linkErr error, codexHome string) string {
	if walkErr != nil {
		return "历史文件检查失败: " + walkErr.Error()
	}
	if linkErr != nil {
		return "目录链接检查失败: " + linkErr.Error()
	}
	if len(paths) > 0 {
		return "发现历史状态: " + strings.Join(paths, ", ")
	}
	if len(links) > 0 {
		return "发现认证以外的宿主链接: " + strings.Join(links, ", ")
	}
	stateFiles, _ := filepath.Glob(filepath.Join(codexHome, "state_*.sqlite"))
	if len(stateFiles) > 0 {
		return "未导入宿主历史、未生成会话历史文件；Codex 只在本轮隔离目录中新建了自身状态数据库"
	}
	return "未导入宿主历史，未生成 sessions、archived_sessions 或 history.jsonl"
}

package skillbundle

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestEmbeddedSkillMatchesInstallableRepositorySkill(t *testing.T) {
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller unavailable")
	}
	source := filepath.Join(filepath.Dir(current), "..", "..", ".agents", "skills", Name)
	wantPaths := []string{"SKILL.md", "agents/openai.yaml", "references/cli-contract.md", "references/error-handling.md", "references/repository-sharing.md", "references/request-patterns.md"}
	paths := Paths()
	if len(paths) != len(wantPaths) {
		t.Fatalf("paths = %#v", paths)
	}
	for index, path := range wantPaths {
		if paths[index] != path {
			t.Fatalf("path[%d] = %q, want %q", index, paths[index], path)
		}
		embedded, err := Read(path)
		if err != nil {
			t.Fatal(err)
		}
		repository, err := os.ReadFile(filepath.Join(source, filepath.FromSlash(path)))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(embedded, repository) {
			t.Fatalf("embedded %s differs from installable Skill", path)
		}
	}
}

func TestSkillIsExplicitInteractiveAndUsesCompletePublicCLI(t *testing.T) {
	openAI, _ := Read("agents/openai.yaml")
	if !bytes.Contains(openAI, []byte("allow_implicit_invocation: false")) {
		t.Fatal("Skill allows implicit invocation")
	}
	skill, _ := Read("SKILL.md")
	for _, required := range []string{"normal interactive Codex workspace", "Before `agent register`", "There is no write mode", "public `peerctx` CLI"} {
		if !bytes.Contains(skill, []byte(required)) {
			t.Fatalf("Skill missing safety rule %q", required)
		}
	}
	contract, _ := Read("references/cli-contract.md")
	for _, command := range []string{"peerctx project create", "peerctx project join", "peerctx project member remove", "peerctx agent register", "peerctx agent remove", "peerctx ask", "peerctx service status", "peerctx skills read", "peerctx version"} {
		if !bytes.Contains(contract, []byte(command)) {
			t.Fatalf("Skill CLI contract missing public command %q", command)
		}
	}
	for _, forbidden := range []string{"internal/relay", "internal/cli", "github.com/wWzZb/peercontext/internal", "peerctx task", "--mode write"} {
		if bytes.Contains(skill, []byte(forbidden)) {
			t.Fatalf("Skill depends on private implementation %q", forbidden)
		}
	}
}

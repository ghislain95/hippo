package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	promptFile    = "../../../docs/prompt/system-prompt.md"
	promptOutFile = "../../internal/analysis/prompt/prompt.gen.json"
)

// fenceOfFixedPart reads the ```text fence under « ## Partie fixe » of source by plain string
// search, independently of PromptCopy: its lines without the fence lines.
func fenceOfFixedPart(t *testing.T, source string) string {
	t.Helper()
	_, section, ok := strings.Cut(source, "\n## Partie fixe\n")
	if !ok {
		t.Fatal("no « ## Partie fixe » section in the prompt")
	}
	_, fence, ok := strings.Cut(section, "\n```text\n")
	if !ok {
		t.Fatal("no ```text fence in « Partie fixe »")
	}
	text, _, ok := strings.Cut(fence, "\n```\n")
	if !ok {
		t.Fatal("the ```text fence of « Partie fixe » is not closed")
	}
	return text
}

func TestPromptCopy(t *testing.T) {
	source, err := os.ReadFile(promptFile)
	if err != nil {
		t.Fatalf("read prompt: %v", err)
	}

	t.Run("the fixed part equals the text fence of Partie fixe byte for byte", func(t *testing.T) {
		raw, err := PromptCopy(source)
		if err != nil {
			t.Fatalf("PromptCopy: %v", err)
		}
		var copied struct {
			PromptVersion int    `json:"prompt_version"`
			Fixed         string `json:"fixed"`
		}
		if err := json.Unmarshal(raw, &copied); err != nil {
			t.Fatalf("parse copy: %v", err)
		}
		if want := fenceOfFixedPart(t, string(source)); copied.Fixed != want {
			t.Errorf("fixed part of %d bytes differs from the fence of %d bytes", len(copied.Fixed), len(want))
		}
		if !strings.HasPrefix(copied.Fixed, "Tu ranges les contenus") || !strings.HasSuffix(copied.Fixed, "content_ok vaut\nfalse.") {
			t.Errorf("fixed part does not span the whole fence: starts %.30q, ends %.30q",
				copied.Fixed, copied.Fixed[max(0, len(copied.Fixed)-30):])
		}
		if copied.PromptVersion != 3 {
			t.Errorf("prompt_version = %d, want 3", copied.PromptVersion)
		}
	})

	t.Run("keeps the tags of the prompt readable", func(t *testing.T) {
		raw, err := PromptCopy(source)
		if err != nil {
			t.Fatalf("PromptCopy: %v", err)
		}
		if !bytes.Contains(raw, []byte("<element>")) {
			t.Error("the copy escapes the < and > of the prompt")
		}
	})

	t.Run("is deterministic", func(t *testing.T) {
		first, err1 := PromptCopy(source)
		second, err2 := PromptCopy(source)
		if err1 != nil || err2 != nil || !bytes.Equal(first, second) {
			t.Errorf("two copies of the same prompt differ (%v, %v)", err1, err2)
		}
	})

	t.Run("matches the committed copy", func(t *testing.T) {
		raw, err := PromptCopy(source)
		if err != nil {
			t.Fatalf("PromptCopy: %v", err)
		}
		committed, err := os.ReadFile(promptOutFile)
		if err != nil {
			t.Fatalf("read committed copy: %v", err)
		}
		if !bytes.Equal(raw, committed) {
			t.Error("the committed copy of the prompt is out of date: run make generate")
		}
	})

	broken := []struct {
		name    string
		mutate  func(string) string
		message string
	}{
		{
			name:    "a missing version fails",
			mutate:  func(s string) string { return strings.Replace(s, "`prompt_version = 3`", "`version 3`", 1) },
			message: "prompt_version",
		},
		{
			name: "two versions fail",
			mutate: func(s string) string {
				return strings.Replace(s, "- **Modèle**", "- `prompt_version = 4`\n- **Modèle**", 1)
			},
			message: "prompt_version",
		},
		{
			name: "a second fence fails",
			mutate: func(s string) string {
				return strings.Replace(s, "\n## Partie variable", "\n```text\nencore\n```\n\n## Partie variable", 1)
			},
			message: "fence",
		},
		{
			name: "a fence of another language fails",
			mutate: func(s string) string {
				return strings.Replace(s, "Identique pour toutes les requêtes ; mise en cache.\n\n```text", "Identique pour toutes les requêtes ; mise en cache.\n\n```markdown", 1)
			},
			message: "text",
		},
		{
			name:    "no fence fails",
			mutate:  func(s string) string { return strings.Replace(s, "## Partie fixe", "## Partie fixe\n\n## Ailleurs", 1) },
			message: "fence",
		},
		{
			name:    "an unclosed fence fails",
			mutate:  func(s string) string { return strings.Replace(s, "false.\n```\n", "false.\n", 1) },
			message: "fence",
		},
		{
			name:    "a missing section fails",
			mutate:  func(s string) string { return strings.Replace(s, "## Partie fixe", "## Partie stable", 1) },
			message: "Partie fixe",
		},
	}
	for _, tc := range broken {
		t.Run(tc.name, func(t *testing.T) {
			mutated := tc.mutate(string(source))
			if mutated == string(source) {
				t.Fatal("the mutation changed nothing: the test no longer matches the prompt")
			}
			if _, err := PromptCopy([]byte(mutated)); err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Errorf("PromptCopy() error = %v, want one naming %q", err, tc.message)
			}
		})
	}
}

func TestRunPromptCheck(t *testing.T) {
	args := func(promptOut string) []string {
		return []string{
			"-schemas", schemasDir, "-examples", examplesDir, "-out", bundleFile, "-openapi-out", openAPIFile,
			"-prompt", promptFile, "-prompt-out", promptOut, "-check",
		}
	}

	t.Run("check fails on a stale copy of the prompt", func(t *testing.T) {
		stale := filepath.Join(t.TempDir(), "prompt.gen.json")
		if err := os.WriteFile(stale, []byte("{}\n"), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		err := run(args(stale), &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), stale) {
			t.Errorf("run -check error = %v, want %s out of date", err, stale)
		}
	})

	t.Run("check passes on the committed copy", func(t *testing.T) {
		if err := run(args(promptOutFile), &bytes.Buffer{}); err != nil {
			t.Errorf("run -check: %v", err)
		}
	})

	t.Run("writes the copy", func(t *testing.T) {
		dir := t.TempDir()
		out := filepath.Join(dir, "prompt.gen.json")
		bundle, components := filepath.Join(dir, "bundle.json"), filepath.Join(dir, "fiches.json")
		err := run([]string{
			"-schemas", schemasDir, "-examples", examplesDir, "-out", bundle, "-openapi-out", components,
			"-prompt", promptFile, "-prompt-out", out,
		}, &bytes.Buffer{})
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		written, err := os.ReadFile(out)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		committed, err := os.ReadFile(promptOutFile)
		if err != nil {
			t.Fatalf("read committed copy: %v", err)
		}
		if !bytes.Equal(written, committed) {
			t.Error("the written copy differs from the committed one")
		}
	})

	t.Run("requires the prompt flags", func(t *testing.T) {
		err := run([]string{"-schemas", schemasDir, "-examples", examplesDir, "-out", bundleFile, "-openapi-out", openAPIFile}, &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "-prompt") {
			t.Errorf("run without -prompt: error = %v", err)
		}
	})
}

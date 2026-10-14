package bundle

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/greadee/review-engine/internal/findings"
	"github.com/greadee/review-engine/internal/reviewers"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bundle.json")
	in := Bundle{
		Version:        Version,
		Profile:        "pr",
		Repository:     "o/r",
		Base:           "a",
		Head:           "b",
		RunID:          "pr@b",
		Generated:      time.Unix(0, 0).UTC(),
		BlockThreshold: "P1",
		Rubric:         []string{"correctness"},
		Files:          []reviewers.FileContent{{Path: "a.go", Content: "package a"}},
		StaticFindings: []findings.Finding{{ID: "x", Title: "t", Severity: findings.P2, Classification: findings.Bug}},
	}
	if err := Save(path, in); err != nil {
		t.Fatal(err)
	}
	out, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if out.Profile != "pr" || len(out.Files) != 1 || out.Files[0].Path != "a.go" || len(out.StaticFindings) != 1 {
		t.Fatalf("roundtrip mismatch: %+v", out)
	}
}

func TestLoadMissing(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.json")); err == nil {
		t.Fatal("expected error for missing bundle")
	}
}

func validBundle() Bundle {
	return Bundle{
		Version:   Version,
		Profile:   "pr",
		RunID:     "pr@abc",
		Head:      "abc",
		Rubric:    []string{"correctness"},
		Files:     []reviewers.FileContent{{Path: "a.go", Content: "package a\n"}},
		Generated: time.Unix(0, 0).UTC(),
	}
}

func TestValidateAcceptsValid(t *testing.T) {
	if err := validBundle().Validate(); err != nil {
		t.Fatalf("valid bundle rejected: %v", err)
	}
}

func TestValidateRejectsTampering(t *testing.T) {
	cases := map[string]func(*Bundle){
		"unknown version":   func(b *Bundle) { b.Version = 99 },
		"missing version":   func(b *Bundle) { b.Version = 0 },
		"unknown profile":   func(b *Bundle) { b.Profile = "root" },
		"empty runId":       func(b *Bundle) { b.RunID = "" },
		"bad threshold":     func(b *Bundle) { b.BlockThreshold = "P9" },
		"path traversal":    func(b *Bundle) { b.Files[0].Path = "../../etc/passwd" },
		"absolute path":     func(b *Bundle) { b.Files[0].Path = "/etc/passwd" },
		"control char path": func(b *Bundle) { b.Files[0].Path = "a\nb.go" },
		"duplicate files":   func(b *Bundle) { b.Files = append(b.Files, b.Files[0]) },
		"oversized file":    func(b *Bundle) { b.Files[0].Content = strings.Repeat("x", MaxFileBytes+1) },
		"bad finding": func(b *Bundle) {
			b.StaticFindings = []findings.Finding{{Title: "", Severity: findings.P1, Classification: findings.Bug}}
		},
		"bad finding sev": func(b *Bundle) {
			b.StaticFindings = []findings.Finding{{Title: "t", Severity: "P9", Classification: findings.Bug}}
		},
		"too many files": func(b *Bundle) {
			b.Files = nil
			for i := 0; i <= MaxFiles; i++ {
				b.Files = append(b.Files, reviewers.FileContent{Path: "f" + string(rune('a'+i%26)) + ".go"})
			}
		},
	}
	for name, tamper := range cases {
		t.Run(name, func(t *testing.T) {
			b := validBundle()
			tamper(&b)
			if err := b.Validate(); err == nil {
				t.Fatalf("expected validation error for %s", name)
			}
		})
	}
}

func TestLoadRejectsUnknownField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bundle.json")
	data, _ := json.Marshal(validBundle())
	// Inject an unknown field a newer/older/hostile producer might add.
	mutated := strings.Replace(string(data), `"version":`, `"evil":true,"version":`, 1)
	if err := os.WriteFile(path, []byte(mutated), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected rejection of unknown bundle field")
	}
}

func TestVerifyBindsToTrustedEvent(t *testing.T) {
	b := Bundle{Repository: "o/r", PR: 3, Head: "abc", Profile: "pr"}
	ok := Trusted{Repository: "o/r", PR: 3, Head: "abc", Profile: "pr"}
	if err := b.Verify(ok); err != nil {
		t.Fatalf("matching bundle rejected: %v", err)
	}
	for name, bad := range map[string]Trusted{
		"repository": {Repository: "o/other"},
		"pr":         {PR: 4},
		"head":       {Head: "def"},
		"profile":    {Profile: "audit"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := b.Verify(bad); err == nil {
				t.Fatalf("expected mismatch on %s", name)
			}
		})
	}
}

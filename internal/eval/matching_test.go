package eval

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/greadee/review-engine/internal/provider"
)

func fixtureByName(t *testing.T, name string) Fixture {
	t.Helper()
	fixtures, err := Load(corpusDir(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fixtures {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("fixture %q not found", name)
	return Fixture{}
}

func TestStripSeedMarkersPreservesLineNumbers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.go")
	content := "package x\n\nfunc F() { // revu-seed: Bug P1 marker-name\n}\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := stripSeedMarkers(dir); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if strings.Contains(got, "revu-seed") || strings.Contains(got, "marker-name") {
		t.Fatalf("seed marker not removed: %q", got)
	}
	if strings.Count(got, "\n") != strings.Count(content, "\n") {
		t.Fatalf("line count changed: %q", got)
	}
	if !strings.Contains(got, "func F() {") {
		t.Fatalf("code line was damaged: %q", got)
	}
}

func TestRealModelMatchingUsesFileAndLine(t *testing.T) {
	fix := fixtureByName(t, "correctness")
	fake := &provider.Fake{Responses: []string{`{"findings":[{"title":"loop reads past the end","classification":"Bug","severity":"P1","file":"svc.go","line":5,"snippet":"for i := 0; i <= len(items); i++ {","impact":"panic","recommendation":"use <","anchor":"model-invented-anchor"}]}`}}
	res, err := runFixture(context.Background(), fix, fake, Options{}, true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Detected != 1 || res.AnchorDetected != 0 || res.FalsePositives != 0 {
		t.Fatalf("semantic match failed: detected=%d anchor=%d falsePositives=%d actual=%+v", res.Detected, res.AnchorDetected, res.FalsePositives, res.Actual)
	}
	if len(res.Actual) != 1 || res.Actual[0].MatchKind != "file+line" {
		t.Fatalf("unexpected match detail: %+v", res.Actual)
	}
}

func TestExactAnchorMatchingStillWins(t *testing.T) {
	fix := fixtureByName(t, "correctness")
	fake := &provider.Fake{Responses: []string{`{"findings":[{"title":"loop reads past the end","classification":"Bug","severity":"P1","file":"svc.go","line":5,"snippet":"for i := 0; i <= len(items); i++ {","impact":"panic","recommendation":"use <","anchor":"off-by-one-total"}]}`}}
	res, err := runFixture(context.Background(), fix, fake, Options{}, true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Detected != 1 || res.AnchorDetected != 1 || res.FalsePositives != 0 {
		t.Fatalf("anchor match failed: detected=%d anchor=%d falsePositives=%d", res.Detected, res.AnchorDetected, res.FalsePositives)
	}
}

func TestSemanticMatchRejectsUnrelatedNearbyClassification(t *testing.T) {
	fix := fixtureByName(t, "test-gap")
	fake := &provider.Fake{Responses: []string{`{"findings":[{"title":"divide by zero","classification":"Bug","severity":"P1","file":"calc.go","line":8,"snippet":"return v * 3","impact":"panic","recommendation":"guard","anchor":"divide-by-zero"}]}`}}
	res, err := runFixture(context.Background(), fix, fake, Options{}, true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Detected != 0 || res.FalsePositives != 1 {
		t.Fatalf("unrelated finding should not satisfy the test-gap expectation: detected=%d falsePositives=%d", res.Detected, res.FalsePositives)
	}
}

func TestSemanticMatchAllowsAlternateTestFile(t *testing.T) {
	fix := fixtureByName(t, "test-gap")
	fake := &provider.Fake{Responses: []string{`{"findings":[{"title":"Triple has no test coverage","classification":"Test","severity":"P3","file":"calc_test.go","line":5,"snippet":"func TestDouble(t *testing.T) {","impact":"regression risk","recommendation":"add a Triple test","anchor":"triple-untested"}]}`}}
	res, err := runFixture(context.Background(), fix, fake, Options{}, true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Detected != 1 || res.FalsePositives != 0 {
		t.Fatalf("alternate test file should match the missing-test expectation: detected=%d falsePositives=%d actual=%+v", res.Detected, res.FalsePositives, res.Actual)
	}
}

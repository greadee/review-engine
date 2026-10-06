package detectors

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"

	"github.com/greadee/review-engine/internal/analyzers"
	"github.com/greadee/review-engine/internal/findings"
)

// OrphanGo flags exported package-level functions whose only references are in
// tests (or that are not referenced at all). This is the "wiring" detector: it
// catches code that exists, compiles, and passes tests but is never composed
// into production.
type OrphanGo struct{}

// NewOrphanGo returns an orphan/wiring detector for Go.
func NewOrphanGo() *OrphanGo { return &OrphanGo{} }

// Name implements analyzers.Analyzer.
func (*OrphanGo) Name() string { return "orphan-go" }

// Analyze implements analyzers.Analyzer.
func (OrphanGo) Analyze(_ context.Context, t analyzers.Target) ([]findings.Finding, error) {
	files := allFiles(t)
	prodUses := map[string]int{}
	testUses := map[string]int{}
	decls := map[string]int{}

	for _, path := range files {
		if isSkippedPath(path) || !strings.HasSuffix(path, ".go") {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, filepath.Join(t.Dir, filepath.FromSlash(path)), nil, 0)
		if err != nil {
			continue
		}
		isTest := strings.HasSuffix(path, "_test.go")
		ast.Inspect(file, func(n ast.Node) bool {
			ident, ok := n.(*ast.Ident)
			if !ok {
				return true
			}
			if isTest {
				testUses[ident.Name]++
			} else {
				prodUses[ident.Name]++
			}
			return true
		})
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Name == nil {
				continue
			}
			decls[fn.Name.Name]++
		}
	}

	var out []findings.Finding
	for _, path := range files {
		if isSkippedPath(path) || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			continue
		}
		if !underReview(t, path) {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, filepath.Join(t.Dir, filepath.FromSlash(path)), nil, 0)
		if err != nil {
			continue
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Name == nil || !fn.Name.IsExported() {
				continue
			}
			name := fn.Name.Name
			// A declaration is one identifier occurrence; anything beyond it is
			// a use in a production file.
			uses := prodUses[name] - decls[name]
			if uses > 0 {
				continue
			}
			line := fset.Position(fn.Pos()).Line
			f := findings.Finding{
				Classification: findings.Maintainability,
				Evidence:       findings.Evidence{File: filepathSlash(path), Line: line, Snippet: "func " + name},
				Detector:       "detector.orphan",
				Anchor:         "orphan:" + name,
				Scope:          strings.TrimSuffix(path, "/"+base(path)),
			}
			if testUses[name] > 0 {
				f.Title = "Exported function has only test callers (not wired into production)"
				f.Severity = findings.P2
				f.Impact = "Behavior is exercised only by tests; production never reaches it."
				f.Recommendation = "Wire the symbol into a composition root, or remove it."
			} else {
				f.Title = "Exported function is unused"
				f.Severity = findings.P3
				f.Impact = "Dead exported API increases surface area."
				f.Recommendation = "Remove the function, or reference it from production."
			}
			out = append(out, f)
			if len(out) >= maxFindings {
				return out, nil
			}
		}
	}
	return out, nil
}

func base(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

func filepathSlash(p string) string { return strings.ReplaceAll(p, "\\", "/") }

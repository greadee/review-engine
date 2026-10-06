package findings

import "testing"

func TestFingerprintDeterministic(t *testing.T) {
	a := Fingerprint("./internal/Foo.go", "semantic.bug", "Nil Check")
	b := Fingerprint("internal/foo.go", "semantic.bug", "nil check")
	if a != b {
		t.Fatalf("fingerprint should normalize path/case/whitespace: %s != %s", a, b)
	}
	if a == Fingerprint("internal/foo.go", "semantic.security", "nil check") {
		t.Fatal("detector must affect fingerprint")
	}
}

func TestSetDedupAndSeverityMerge(t *testing.T) {
	s := NewSet()
	s.Add(Finding{Title: "a", Detector: "d", Anchor: "x", Severity: P2, Classification: Bug, Evidence: Evidence{File: "f.go"}})
	s.Add(Finding{Title: "a", Detector: "d", Anchor: "x", Severity: P1, Classification: Security, Evidence: Evidence{File: "f.go"}})
	if s.Len() != 1 {
		t.Fatalf("expected 1 deduped finding, got %d", s.Len())
	}
	got := s.Items()[0]
	if got.Severity != P1 {
		t.Fatalf("merge should keep more severe: got %s", got.Severity)
	}
	if got.ID == "" {
		t.Fatal("ID should be computed")
	}
}

func TestSetSortAndBlocking(t *testing.T) {
	s := NewSet()
	s.Add(Finding{Title: "low", Detector: "d", Anchor: "1", Severity: P3, Classification: Bug, Evidence: Evidence{File: "b.go", Line: 2}})
	s.Add(Finding{Title: "high", Detector: "d", Anchor: "2", Severity: P0, Classification: Security, Evidence: Evidence{File: "a.go", Line: 9}})
	s.Add(Finding{Title: "mid", Detector: "d", Anchor: "3", Severity: P2, Classification: Test, Evidence: Evidence{File: "a.go", Line: 1}})
	items := s.Items()
	if items[0].Title != "high" || items[2].Title != "low" {
		t.Fatalf("expected severity order, got %v", []string{items[0].Title, items[1].Title, items[2].Title})
	}
	if len(s.Blocking(P1)) != 1 {
		t.Fatalf("expected 1 blocking, got %d", len(s.Blocking(P1)))
	}
}

func TestLifecycleTransitions(t *testing.T) {
	f := Finding{Title: "t", Detector: "d", Anchor: "a", Severity: P1, Classification: Bug, Evidence: Evidence{File: "f.go"}}
	f.EnsureID()

	first := Lifecycle(nil, []Finding{f}, "run1")
	if first[0].Status != StatusNew || first[0].FirstSeen != "run1" {
		t.Fatalf("first run should be new: %+v", first[0])
	}
	second := Lifecycle(first, []Finding{f}, "run2")
	if second[0].Status != StatusOngoing || second[0].FirstSeen != "run1" || second[0].LastSeen != "run2" {
		t.Fatalf("second run should be ongoing with preserved firstSeen: %+v", second[0])
	}
	// Disappears then returns -> regressed.
	resolved := Compare(second, nil)
	if len(resolved) != 1 || resolved[0].Status != StatusResolved {
		t.Fatalf("expected resolved: %+v", resolved)
	}
	third := Lifecycle(resolved, []Finding{f}, "run3")
	if third[0].Status != StatusRegressed {
		t.Fatalf("expected regressed, got %s", third[0].Status)
	}
}

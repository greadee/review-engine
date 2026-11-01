package profiles

import "testing"

func TestBuiltInProfilesAreComplete(t *testing.T) {
	all := All()
	if len(all) != 5 {
		t.Fatalf("expected 5 built-in profiles, got %v", all)
	}
	for _, name := range all {
		if !Valid(name) {
			t.Errorf("profile %q is not valid", name)
		}
		if rubric := Rubric(name); len(rubric) == 0 {
			t.Errorf("profile %q has an empty rubric", name)
		}
	}
	if Valid("unknown") {
		t.Fatal("unknown profile must not be valid")
	}
}

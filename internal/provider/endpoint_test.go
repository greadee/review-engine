package provider

import "testing"

func TestCheckEndpoint(t *testing.T) {
	allowed := []string{DefaultBaseURL, "https://internal.example/v1"}
	cases := []struct {
		name    string
		baseURL string
		wantErr bool
	}{
		{"empty uses default", "", false},
		{"default", DefaultBaseURL, false},
		{"default with trailing slash", DefaultBaseURL + "/", false},
		{"allowed custom", "https://internal.example/v1", false},
		{"case and slash insensitive", "HTTPS://Internal.Example/v1/", false},
		{"attacker", "https://attacker.example/v1", true},
		{"subdomain of allowed", "https://internal.example.evil/v1", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckEndpoint(tc.baseURL, allowed)
			if (err != nil) != tc.wantErr {
				t.Fatalf("CheckEndpoint(%q) error = %v, wantErr %v", tc.baseURL, err, tc.wantErr)
			}
		})
	}
}

func TestSplitBaseURLs(t *testing.T) {
	got := SplitBaseURLs(" https://a.example/v1 ,https://b.example/v1\nhttps://c.example/v1 ")
	want := []string{"https://a.example/v1", "https://b.example/v1", "https://c.example/v1"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestTrustedBaseURLsIncludesDefaultAndEnv(t *testing.T) {
	t.Setenv(AllowedBaseURLsEnv, "https://x.example/v1")
	got := TrustedBaseURLs()
	if len(got) < 2 || got[0] != DefaultBaseURL {
		t.Fatalf("trusted list should start with the default: %v", got)
	}
	found := false
	for _, u := range got {
		if u == "https://x.example/v1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("env allowlist not included: %v", got)
	}
}

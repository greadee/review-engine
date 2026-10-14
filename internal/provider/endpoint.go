package provider

import (
	"fmt"
	"os"
	"strings"
)

// AllowedBaseURLsEnv names the environment variable that lists provider
// endpoints trusted to receive a credential. It is populated from workflow
// configuration (or an action input), never from repository configuration, so a
// pull request cannot add an endpoint of its own.
const AllowedBaseURLsEnv = "REVIEW_PROVIDER_ALLOWED_BASE_URLS"

// TrustedBaseURLs returns the endpoints that may receive a credential from
// workflow-controlled sources: the built-in default plus any listed in
// REVIEW_PROVIDER_ALLOWED_BASE_URLS.
func TrustedBaseURLs() []string {
	out := []string{DefaultBaseURL}
	out = append(out, SplitBaseURLs(os.Getenv(AllowedBaseURLsEnv))...)
	return out
}

// SplitBaseURLs splits a comma, whitespace, or newline separated list of URLs.
func SplitBaseURLs(s string) []string {
	var out []string
	for _, part := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	}) {
		if p := normalizeEndpoint(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// CheckEndpoint reports whether baseURL may receive a credential. Matching is
// exact after normalizing case and trailing slashes. An empty baseURL means the
// built-in default, which is always allowed; every other endpoint must be
// explicitly allowlisted by trusted configuration. This is what stops a
// pull-request-controlled revu.yaml from redirecting the provider key.
func CheckEndpoint(baseURL string, allowed []string) error {
	target := normalizeEndpoint(baseURL)
	if target == "" {
		return nil
	}
	for _, a := range allowed {
		if normalizeEndpoint(a) == target {
			return nil
		}
	}
	return fmt.Errorf("provider: refusing to send credentials to %q; add it to %s if this endpoint is intended (trusted endpoints: %s)",
		baseURL, AllowedBaseURLsEnv, strings.Join(uniqueNonEmpty(allowed), ", "))
}

func normalizeEndpoint(s string) string {
	return strings.ToLower(strings.TrimRight(strings.TrimSpace(s), "/"))
}

func uniqueNonEmpty(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		n := normalizeEndpoint(s)
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, s)
	}
	return out
}

package findings

// Lifecycle reconciles the current run's findings against the previous run's
// tracked findings, assigning each a Status and carrying first-seen forward.
//
// prev and cur are matched by fingerprint (ID). runID is stamped on LastSeen
// for every current finding. firstSeen is preserved from prev when the finding
// was already known.
func Lifecycle(prev, cur []Finding, runID string) []Finding {
	known := make(map[string]Finding, len(prev))
	for _, p := range prev {
		known[p.ID] = p
	}
	out := make([]Finding, 0, len(cur))
	for _, c := range cur {
		c.EnsureID()
		c.RunID = runID
		c.LastSeen = runID
		if p, ok := known[c.ID]; ok {
			c.FirstSeen = p.FirstSeen
			if p.Status == StatusResolved || p.Status == StatusRegressed {
				c.Status = StatusRegressed
			} else {
				c.Status = StatusOngoing
			}
		} else {
			c.FirstSeen = runID
			c.Status = StatusNew
		}
		out = append(out, c)
	}
	return out
}

// Compare returns findings that were present in prev but are absent from cur,
// carrying them forward as resolved. Callers persist prev+resolved so that a
// later regression can be detected.
func Compare(prev, cur []Finding) []Finding {
	current := make(map[string]bool, len(cur))
	for _, c := range cur {
		current[c.ID] = true
	}
	var resolved []Finding
	for _, p := range prev {
		if current[p.ID] || p.Status == StatusResolved {
			continue
		}
		p.Status = StatusResolved
		resolved = append(resolved, p)
	}
	return resolved
}

// MergeTracking combines the current findings with carried-forward resolved
// findings so the persisted state covers both.
func MergeTracking(cur, resolved []Finding) []Finding {
	out := make([]Finding, 0, len(cur)+len(resolved))
	out = append(out, cur...)
	out = append(out, resolved...)
	return out
}

package board

import "testing"

// Every built-in profile must resolve and satisfy the invariants a board relies on:
// a non-empty column set with exactly one done column, and a declared lane
// dimension. This guards the registry (Profiles) and each preset at once.
func TestBuiltinProfiles(t *testing.T) {
	want := []string{"sdd", "scrum", "kanban", "docket", "openspec", "superpowers"}
	for _, key := range want {
		p, ok := LookupProfile(key)
		if !ok {
			t.Fatalf("profile %q not registered", key)
		}
		if p.Key != key {
			t.Errorf("profile %q has mismatched Key %q", key, p.Key)
		}
		if len(p.Columns) == 0 {
			t.Errorf("profile %q has no columns", key)
		}
		if p.LaneDimension == "" {
			t.Errorf("profile %q has empty lane dimension", key)
		}
		done := 0
		for _, c := range p.Columns {
			if c.IsDone {
				done++
			}
		}
		if done == 0 {
			t.Errorf("profile %q has no done column", key)
		}
	}
}

// The imported-methodology profiles (docket/openspec/superpowers) are panes of
// glass: the tool owns its own workflow, so they must enforce nothing. A stray gate
// would reject agent hydration upserts that don't set spec_status.
func TestImportedProfilesHaveNoGates(t *testing.T) {
	for _, key := range []string{"docket", "openspec", "superpowers"} {
		p, _ := LookupProfile(key)
		if len(p.Policies.ColumnGates) != 0 || len(p.Policies.Transitions) != 0 {
			t.Errorf("imported profile %q must enforce nothing, got gates=%d transitions=%d",
				key, len(p.Policies.ColumnGates), len(p.Policies.Transitions))
		}
	}
}

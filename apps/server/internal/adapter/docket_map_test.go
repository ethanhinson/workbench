package adapter

import "testing"

// mkChange builds a Change with a possibly-set PR scalar.
func mkChange(mut func(*Change)) Change {
	ch := Change{ID: 1, Type: "feat", Priority: "medium", Status: "proposed"}
	if mut != nil {
		mut(&ch)
	}
	return ch
}

func withPR(v string) func(*Change) { return func(c *Change) { c.PR = rawScalar{v: v} } }

func TestPlaceChange(t *testing.T) {
	cases := []struct {
		name   string
		mut    func(*Change)
		column string
	}{
		// Terminal states win over everything.
		{"done", func(c *Change) { c.Status = "done" }, ColDone},
		{"killed", func(c *Change) { c.Status = "killed" }, ColKilled},
		{"deferred", func(c *Change) { c.Status = "deferred" }, ColDeferred},
		{"done beats a branch", func(c *Change) { c.Status = "done"; c.Branch = "b" }, ColDone},
		{"done beats a pr", func(c *Change) { c.Status = "done"; c.PR = rawScalar{v: "5"} }, ColDone},

		// In-flight: pr → review, branch → in_progress, pr beats branch.
		{"pr int → review", withPR("5"), ColReview},
		{"pr url → review", withPR("https://github.com/x/y/pull/4"), ColReview},
		{"branch → in_progress", func(c *Change) { c.Branch = "feat/x" }, ColInProgress},
		{"pr beats branch", func(c *Change) { c.Branch = "feat/x"; c.PR = rawScalar{v: "5"} }, ColReview},
		{"status in_progress w/o branch/pr", func(c *Change) { c.Status = "in_progress" }, ColInProgress},

		// Proposed gradient; trivial precedes spec/plan.
		{"trivial → build-ready", func(c *Change) { c.Trivial = true }, ColSpecd},
		{"trivial+branch → branch wins", func(c *Change) { c.Trivial = true; c.Branch = "b" }, ColInProgress},
		{"spec+plan → build-ready", func(c *Change) { c.Spec = "s.md"; c.Plan = "p.md" }, ColSpecd},
		{"spec only → specifying", func(c *Change) { c.Spec = "s.md" }, ColSpecifying},
		{"no spec → backlog", nil, ColBacklog},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := PlaceChange(mkChange(c.mut))
			if got.ColumnKey != c.column {
				t.Fatalf("column = %q, want %q", got.ColumnKey, c.column)
			}
		})
	}
}

func TestPlaceChangeBlocked(t *testing.T) {
	if PlaceChange(mkChange(nil)).Blocked {
		t.Fatal("unblocked change reported blocked")
	}
	if !PlaceChange(mkChange(func(c *Change) { c.BlockedBy = "2" })).Blocked {
		t.Fatal("blocked_by set but Blocked is false")
	}
}

func TestTranslatePriority(t *testing.T) {
	cases := map[string]string{"high": "p1", "medium": "p2", "low": "p3", "": "p2", "garbage": "p2"}
	for in, want := range cases {
		if got := translatePriority(in); got != want {
			t.Errorf("translatePriority(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeType(t *testing.T) {
	if got := normalizeType(""); got != "change" {
		t.Errorf("empty type → %q, want change", got)
	}
	for _, ty := range []string{"feat", "fix", "chore", "docs", "refactor"} {
		if got := normalizeType(ty); got != ty {
			t.Errorf("normalizeType(%q) = %q", ty, got)
		}
	}
}

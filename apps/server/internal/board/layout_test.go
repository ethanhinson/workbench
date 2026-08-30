package board

import "testing"

// validLayout is a column-owning layout: each placement view owns real profile
// columns, and every column is owned by exactly one view (unambiguous routing).
func validLayout() Layout {
	return Layout{
		Nav: []NavItem{
			{ID: "flow", Label: "In Flight", View: "flow"},
			{ID: "specs", Label: "Specs", View: "specs"},
			{ID: "done", Label: "Done", View: "done"},
		},
		Views: map[string]LayoutView{
			"flow": {
				Type:    ViewLanes,
				Columns: []LayoutAxis{{Key: "todo", Label: "To Do"}, {Key: "doing", Label: "Doing"}},
			},
			"specs": {Type: ViewDoc},
			"done":  {Type: ViewList, Columns: []LayoutAxis{{Key: "done", Label: "Done"}}},
		},
	}
}

// profileCols is a small profile column set for ValidateForProfile tests.
func profileCols() []ColumnDef {
	return []ColumnDef{
		{Key: "todo", Name: "To Do"},
		{Key: "doing", Name: "Doing"},
		{Key: "done", Name: "Done", IsDone: true},
	}
}

func TestLayoutValidate(t *testing.T) {
	if err := validLayout().Validate(); err != nil {
		t.Fatalf("valid layout rejected: %v", err)
	}

	cases := []struct {
		name string
		mut  func(*Layout)
	}{
		{"no nav", func(l *Layout) { l.Nav = nil }},
		{"nav -> unknown view", func(l *Layout) { l.Nav[0].View = "ghost" }},
		{"duplicate nav id", func(l *Layout) { l.Nav[1].ID = "flow" }},
		{"unknown view type", func(l *Layout) { v := l.Views["done"]; v.Type = "grid"; l.Views["done"] = v }},
		{"placement view without columns", func(l *Layout) { v := l.Views["flow"]; v.Columns = nil; l.Views["flow"] = v }},
		{"duplicate column key", func(l *Layout) {
			v := l.Views["flow"]
			v.Columns = []LayoutAxis{{Key: "x", Label: "X"}, {Key: "x", Label: "X2"}}
			l.Views["flow"] = v
		}},
	}
	for _, c := range cases {
		lo := validLayout()
		c.mut(&lo)
		if err := lo.Validate(); err == nil {
			t.Errorf("%s: expected validation error, got nil", c.name)
		}
	}
}

func TestValidateForProfile(t *testing.T) {
	// The valid layout owns todo/doing/done — all real, each once.
	unowned, err := validLayout().ValidateForProfile(profileCols())
	if err != nil {
		t.Fatalf("valid layout rejected by profile check: %v", err)
	}
	if len(unowned) != 0 {
		t.Fatalf("expected no unowned columns, got %v", unowned)
	}

	// Unknown column key → error.
	lo := validLayout()
	v := lo.Views["flow"]
	v.Columns = append(v.Columns, LayoutAxis{Key: "bogus", Label: "Bogus"})
	lo.Views["flow"] = v
	if _, err := lo.ValidateForProfile(profileCols()); err == nil {
		t.Error("owning a non-profile column should error")
	}

	// A column owned by two views → error (ambiguous nav routing).
	lo = validLayout()
	v = lo.Views["done"]
	v.Columns = []LayoutAxis{{Key: "doing", Label: "Doing again"}}
	lo.Views["done"] = v
	if _, err := lo.ValidateForProfile(profileCols()); err == nil {
		t.Error("a column owned by two views should error")
	}

	// An unowned column is not an error, but is reported.
	lo = validLayout()
	v = lo.Views["flow"]
	v.Columns = []LayoutAxis{{Key: "todo", Label: "To Do"}} // drop 'doing'
	lo.Views["flow"] = v
	unowned, err = lo.ValidateForProfile(profileCols())
	if err != nil {
		t.Fatalf("dropping a column should warn, not error: %v", err)
	}
	if len(unowned) != 1 || unowned[0] != "doing" {
		t.Fatalf("expected 'doing' unowned, got %v", unowned)
	}
}

func TestPlacementLabelsAllowed(t *testing.T) {
	// view/lane/column stay open namespaces (inert legacy labels), still allowed.
	for _, ns := range []string{"view", "lane", "column"} {
		if err := ValidateLabel(Label{NS: ns, Value: "anything"}, nil); err != nil {
			t.Errorf("label %s:anything should be allowed: %v", ns, err)
		}
	}
	if err := ValidateLabel(Label{NS: "bogus", Value: "x"}, nil); err == nil {
		t.Fatal("unknown namespace should still be rejected")
	}
}

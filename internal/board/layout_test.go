package board

import "testing"

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
				Lanes:   []LayoutAxis{{Key: "build", Label: "Build"}, {Key: "review", Label: "Review"}},
				Columns: []LayoutAxis{{Key: "todo", Label: "To Do"}, {Key: "doing", Label: "Doing"}},
			},
			"specs": {Type: ViewDoc},
			"done":  {Type: ViewList},
		},
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
		{"lanes view without lanes", func(l *Layout) { v := l.Views["flow"]; v.Lanes = nil; l.Views["flow"] = v }},
		{"duplicate lane key", func(l *Layout) {
			v := l.Views["flow"]
			v.Lanes = []LayoutAxis{{Key: "x", Label: "X"}, {Key: "x", Label: "X2"}}
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

func TestBoardViewNeedsAxes(t *testing.T) {
	// A board view needs lanes but not columns.
	lo := Layout{
		Nav:   []NavItem{{ID: "b", Label: "B", View: "b"}},
		Views: map[string]LayoutView{"b": {Type: ViewBoard, Lanes: []LayoutAxis{{Key: "l", Label: "L"}}}},
	}
	if err := lo.Validate(); err != nil {
		t.Fatalf("board view with lanes should be valid: %v", err)
	}
	// ...without lanes it's invalid.
	lo.Views["b"] = LayoutView{Type: ViewBoard}
	if err := lo.Validate(); err == nil {
		t.Fatal("board view without lanes should be invalid")
	}
}

func TestIncludeView(t *testing.T) {
	v := LayoutView{Type: ViewList}
	if got := v.IncludeView("done"); got != "done" {
		t.Fatalf("default include should be the view id, got %q", got)
	}
	v.Include = &ViewInclude{View: "backlog"}
	if got := v.IncludeView("done"); got != "backlog" {
		t.Fatalf("explicit include should win, got %q", got)
	}
}

func TestPlacementLabelsAllowed(t *testing.T) {
	// view/lane/column are open namespaces now.
	for _, ns := range []string{"view", "lane", "column"} {
		if err := ValidateLabel(Label{NS: ns, Value: "anything"}, nil); err != nil {
			t.Errorf("label %s:anything should be allowed: %v", ns, err)
		}
	}
	if err := ValidateLabel(Label{NS: "bogus", Value: "x"}, nil); err == nil {
		t.Fatal("unknown namespace should still be rejected")
	}
}

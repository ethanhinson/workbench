package adapter

import (
	"strings"
	"testing"
)

func TestParseChangeBasic(t *testing.T) {
	src := `---
id: 3
slug: package-distribution
title: Package distribution
status: proposed
priority: medium
type: chore
depends_on: []
related: [2]
trivial: true
pr:
---

## Why

Body content here.
`
	ch, err := ParseChange([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if ch.ID != 3 || ch.Slug != "package-distribution" || ch.Type != "chore" {
		t.Fatalf("frontmatter mis-parsed: %+v", ch)
	}
	if !ch.Trivial {
		t.Fatal("trivial should be true")
	}
	if ch.PR.Set() {
		t.Fatal("empty pr should not be Set()")
	}
	if len(ch.Related) != 1 || ch.Related[0] != 2 {
		t.Fatalf("related = %v, want [2]", ch.Related)
	}
	if !strings.Contains(ch.Body, "Body content here.") {
		t.Fatalf("body not extracted: %q", ch.Body)
	}
}

func TestParseChangePRVariants(t *testing.T) {
	intPR := `---
id: 1
pr: 5
---
x`
	ch, err := ParseChange([]byte(intPR))
	if err != nil || !ch.PR.Set() || ch.PR.String() != "5" {
		t.Fatalf("int pr: set=%v val=%q err=%v", ch.PR.Set(), ch.PR.String(), err)
	}

	urlPR := `---
id: 1
pr: https://github.com/x/y/pull/4
---
x`
	ch, err = ParseChange([]byte(urlPR))
	if err != nil || !ch.PR.Set() || !strings.HasPrefix(ch.PR.String(), "https://") {
		t.Fatalf("url pr: set=%v val=%q err=%v", ch.PR.Set(), ch.PR.String(), err)
	}
}

func TestParseChangeNoFence(t *testing.T) {
	if _, err := ParseChange([]byte("no frontmatter here\njust text")); err == nil {
		t.Fatal("expected error for a file with no frontmatter fence")
	}
}

func TestParseChangeUnterminated(t *testing.T) {
	if _, err := ParseChange([]byte("---\nid: 1\nnever closes\n")); err == nil {
		t.Fatal("expected error for an unclosed frontmatter fence")
	}
}

func TestParseChangeEmptyArrays(t *testing.T) {
	src := "---\nid: 1\ndepends_on: []\nrelated: []\n---\n"
	ch, err := ParseChange([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(ch.DependsOn) != 0 || len(ch.Related) != 0 {
		t.Fatalf("empty arrays mis-parsed: %+v", ch)
	}
}

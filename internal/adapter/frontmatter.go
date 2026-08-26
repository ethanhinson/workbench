package adapter

import (
	"bytes"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Change is one docket change manifest: its YAML frontmatter plus the markdown
// body. Fields mirror the docket convention; unknown fields are ignored.
type Change struct {
	ID         int       `yaml:"id"`
	Slug       string    `yaml:"slug"`
	Title      string    `yaml:"title"`
	Status     string    `yaml:"status"`   // proposed|in_progress|done|killed|deferred
	Priority   string    `yaml:"priority"` // low|medium|high
	Type       string    `yaml:"type"`     // feat|fix|chore|refactor|docs|…
	Created    string    `yaml:"created"`
	Updated    string    `yaml:"updated"`
	DependsOn  []int     `yaml:"depends_on"`
	Related    []int     `yaml:"related"`
	Spec       string    `yaml:"spec"`
	Plan       string    `yaml:"plan"`
	Results    string    `yaml:"results"`
	Trivial    bool      `yaml:"trivial"`
	Branch     string    `yaml:"branch"`
	PR         rawScalar `yaml:"pr"` // int (5) or URL string; .Set() = non-empty
	BlockedBy  string    `yaml:"blocked_by"`
	Reconciled bool      `yaml:"reconciled"`
	Body       string    `yaml:"-"` // markdown after the frontmatter → item Content
}

// rawScalar holds a YAML scalar that may be an int or a string (docket's `pr` is
// `5` in some manifests, a full PR URL in others). It normalizes to a string.
type rawScalar struct{ v string }

func (r *rawScalar) UnmarshalYAML(node *yaml.Node) error {
	r.v = strings.TrimSpace(node.Value)
	return nil
}

// Set reports whether the scalar carries a value (a PR/branch is "set").
func (r rawScalar) Set() bool { return r.v != "" }

// String returns the scalar as text.
func (r rawScalar) String() string { return r.v }

// ParseChange splits a change file into its `---`-fenced frontmatter and body,
// decodes the frontmatter as YAML, and attaches the body as Content. A file with
// no frontmatter fence is an error (the caller skips it) rather than a panic.
func ParseChange(data []byte) (Change, error) {
	front, body, err := splitFrontmatter(data)
	if err != nil {
		return Change{}, err
	}
	var ch Change
	if err := yaml.Unmarshal(front, &ch); err != nil {
		return Change{}, fmt.Errorf("decode frontmatter: %w", err)
	}
	ch.Body = strings.TrimSpace(string(body))
	return ch, nil
}

// splitFrontmatter separates a leading `---\n … \n---\n` YAML block from the
// markdown that follows. Tolerates a leading BOM/blank lines before the fence.
func splitFrontmatter(data []byte) (front, body []byte, err error) {
	s := bytes.TrimPrefix(data, []byte("\uFEFF"))
	s = bytes.TrimLeft(s, " \t\r\n")
	if !bytes.HasPrefix(s, []byte("---")) {
		return nil, nil, fmt.Errorf("no frontmatter fence")
	}
	// Drop the opening fence line.
	nl := bytes.IndexByte(s, '\n')
	if nl < 0 {
		return nil, nil, fmt.Errorf("frontmatter fence not terminated")
	}
	rest := s[nl+1:]
	// Find the closing fence at the start of a line.
	end := findClosingFence(rest)
	if end < 0 {
		return nil, nil, fmt.Errorf("frontmatter fence not closed")
	}
	front = rest[:end]
	// Skip past the closing fence line.
	after := rest[end:]
	if nl2 := bytes.IndexByte(after, '\n'); nl2 >= 0 {
		body = after[nl2+1:]
	}
	return front, body, nil
}

// findClosingFence returns the byte offset of a `---` line (a fence at column 0),
// or -1. It matches only a line that is exactly the fence marker.
func findClosingFence(b []byte) int {
	off := 0
	for len(b) > 0 {
		var line []byte
		if nl := bytes.IndexByte(b, '\n'); nl >= 0 {
			line = b[:nl]
		} else {
			line = b
		}
		if t := bytes.TrimRight(line, "\r"); string(t) == "---" {
			return off
		}
		nl := bytes.IndexByte(b, '\n')
		if nl < 0 {
			break
		}
		off += nl + 1
		b = b[nl+1:]
	}
	return -1
}

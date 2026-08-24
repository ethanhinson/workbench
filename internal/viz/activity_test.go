package viz

import "testing"

func TestCleanCommand(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		// The common harness wrapper: cd into the repo, then run the real command.
		{"cd /Users/x/dev/repo && go test ./...", "go test ./..."},
		{"cd /a/b && git status --short", "git status --short"},
		// Semicolon connector.
		{"cd /a/b ; ls -la", "ls -la"},
		// Nested cds (some wrappers stack them) collapse to the final command.
		{"cd /a && cd /b && echo hi", "echo hi"},
		// A bare cd with no following command stays intact (still reads as something).
		{"cd /Users/x/dev/repo", "cd /Users/x/dev/repo"},
		// No wrapper: unchanged.
		{"go build ./...", "go build ./..."},
		// Leading/trailing space is trimmed.
		{"  cd /a &&   ls  ", "ls"},
		// Not a cd prefix (a command that merely contains 'cd' later) is untouched.
		{"echo cd /a && ls", "echo cd /a && ls"},
	}
	for _, c := range cases {
		if got := cleanCommand(c.in); got != c.want {
			t.Errorf("cleanCommand(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

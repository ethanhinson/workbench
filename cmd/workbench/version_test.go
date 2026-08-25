package main

import (
	"strings"
	"testing"
)

// writeVersion should print the stamped version and nothing else but a trailing
// newline, so `workbench --version` is greppable and the release binaries are
// self-identifying.
func TestWriteVersion(t *testing.T) {
	old := version
	t.Cleanup(func() { version = old })

	version = "v1.2.3"
	var b strings.Builder
	writeVersion(&b)

	got := b.String()
	if want := "workbench v1.2.3\n"; got != want {
		t.Fatalf("writeVersion = %q, want %q", got, want)
	}
}

// The default when unstamped (a plain `go build`) is "dev", not an empty string,
// so a version line is always present.
func TestVersionDefault(t *testing.T) {
	if version == "" {
		t.Fatal("version default is empty; want a non-empty fallback like \"dev\"")
	}
}

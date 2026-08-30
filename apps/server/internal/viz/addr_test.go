package viz

import "testing"

// TestNormalizeAddr pins the bind-exposure rule: an address that names no host
// defaults to loopback, and an address that names a host binds exactly what it
// names. The rule keys on "did the operator name a host?" — never on whether the
// named host happens to be local — so 0.0.0.0 stays a conscious opt-in.
func TestNormalizeAddr(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		// No host named => default to loopback.
		{"port only", ":7777", "127.0.0.1:7777"},
		{"bare port", "7777", "127.0.0.1:7777"},

		// Host named => bind exactly what was named.
		{"all interfaces v4", "0.0.0.0:7777", "0.0.0.0:7777"},
		{"lan address", "192.168.1.5:7777", "192.168.1.5:7777"},
		{"localhost", "localhost:7777", "localhost:7777"},
		{"all interfaces v6", "[::]:7777", "[::]:7777"},
		{"loopback v6", "[::1]:7777", "[::1]:7777"},
		{"already loopback", "127.0.0.1:7777", "127.0.0.1:7777"},

		// The disabled sentinel must survive intact.
		{"empty", "", ""},

		// Malformed input is passed through untouched: the helper never invents
		// an address out of junk, it lets ListenAndServe report the real error.
		{"junk", "not-an-address", "not-an-address"},
		{"too many colons", "1.2.3.4:70:80", "1.2.3.4:70:80"},
		{"unbracketed v6", "::1", "::1"},
		{"non-ascii digits", "٧٧٧٧", "٧٧٧٧"},
		{"port with space", "7777 ", "7777 "},
		{"trailing colon host named", "0.0.0.0:", "0.0.0.0:"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeAddr(tt.in); got != tt.want {
				t.Errorf("NormalizeAddr(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestNormalizeAddrNeverWidens is the security-facing invariant behind the table:
// normalization may only narrow exposure. Whatever the input, the result is never
// a host-less address, and a named host is never replaced by a different one.
func TestNormalizeAddrNeverWidens(t *testing.T) {
	for _, in := range []string{":7777", "7777", "0.0.0.0:7777", "[::]:7777", "localhost:80", ""} {
		got := NormalizeAddr(in)
		if got == in {
			continue // unchanged can never widen
		}
		if got != "127.0.0.1:"+trimLeadingColon(in) {
			t.Errorf("NormalizeAddr(%q) = %q: rewrote to something other than loopback", in, got)
		}
	}
}

func trimLeadingColon(s string) string {
	if len(s) > 0 && s[0] == ':' {
		return s[1:]
	}
	return s
}

package viz

import "net"

// loopback is the host the viz server binds when the operator named none.
const loopback = "127.0.0.1"

// NormalizeAddr applies the viz bind-exposure default to a listen address: an
// address that names no host binds loopback only, and an address that names a
// host binds exactly what it names.
//
// The rule keys strictly on "did the operator name a host?" — never on whether
// the named host happens to be local. Only an absent host is defaulted, so
// exposing the server beyond the machine (":7777" -> "0.0.0.0:7777") stays a
// conscious opt-in rather than an accident of omission.
//
//	":7777"            -> "127.0.0.1:7777"   port only, no host named
//	"7777"             -> "127.0.0.1:7777"   bare port, same intent
//	"0.0.0.0:7777"     -> unchanged          explicit opt-in
//	"192.168.1.5:7777" -> unchanged          explicit host
//	"localhost:7777"   -> unchanged          explicit, and already local
//	"[::]:7777"        -> unchanged          explicit all-interfaces v6
//	""                 -> ""                 the "disabled" sentinel
//
// It never errors and never invents an address out of a malformed one: anything
// it cannot read as host-less is returned untouched, so net.Listen reports the
// real problem with the operator's own input. Normalization therefore only ever
// narrows exposure — it can add a loopback host, never substitute a wider one.
func NormalizeAddr(addr string) string {
	if addr == "" {
		return "" // disabled; the caller decides what that means
	}
	if host, port, err := net.SplitHostPort(addr); err == nil {
		if host == "" {
			return net.JoinHostPort(loopback, port)
		}
		return addr // a host was named — bind exactly it
	}
	// The split failed: either a bare port ("7777"), which names no host and so
	// gets the same loopback default, or genuine junk, which is passed through.
	if isPort(addr) {
		return net.JoinHostPort(loopback, addr)
	}
	return addr
}

// isPort reports whether s is a non-empty run of ASCII digits, the only shape a
// bare port can take. It deliberately does not accept unicode digits or
// surrounding space, neither of which net.Listen would accept as a port either.
func isPort(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// Package update decides whether a newer lk has been published and how the
// current installation can get it.
package update

import "strings"

// Versions reaching this package come from three places that disagree on the
// leading v: the ldflag injects {{.Version}} bare ("0.7.0"), git tags and the
// releases/latest redirect carry it ("v0.7.0"), and the generated cask carries
// it bare again. Ordering them by hand (rather than adding a semver module)
// keeps go.mod at its current five direct dependencies — the same bar that
// rejected gojq — and the shapes are narrow because our own release workflow
// validates every tag against ^v\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$.

// parsed is a version broken into the parts that determine precedence.
type parsed struct {
	nums [3]int
	pre  []string // prerelease identifiers; empty for a final release
}

// parse splits a version like "v1.2.3-rc.1+meta" into its parts. ok is false
// for anything that must not be ordered: development builds ("dev"), empty
// strings, and malformed input. Refusing beats guessing here — a version this
// package cannot read would otherwise be announced as an available update.
func parse(v string) (parsed, bool) {
	var p parsed

	v = strings.TrimPrefix(v, "v")
	if v == "" {
		return p, false
	}
	// Build metadata is ignored for precedence (semver §10) but still has to be
	// well formed: this package refuses what it cannot read rather than ordering
	// it anyway.
	if i := strings.IndexByte(v, '+'); i >= 0 {
		if !validIdentifiers(v[i+1:]) {
			return p, false
		}
		v = v[:i]
	}

	core := v
	if i := strings.IndexByte(v, '-'); i >= 0 {
		core, v = v[:i], v[i+1:]
		if v == "" {
			return p, false
		}
		if !validIdentifiers(v) {
			return p, false
		}
		p.pre = strings.Split(v, ".")
	}

	fields := strings.Split(core, ".")
	if len(fields) != 3 {
		return p, false
	}
	for i, f := range fields {
		n, ok := numericID(f)
		if !ok {
			return p, false
		}
		p.nums[i] = n
	}
	return p, true
}

// validIdentifiers reports whether a dot-separated identifier list is well
// formed: each part non-empty, restricted to [0-9A-Za-z-], and — when it is
// all digits — free of a leading zero (semver §9).
func validIdentifiers(list string) bool {
	for _, id := range strings.Split(list, ".") {
		if id == "" {
			return false
		}
		numeric := true
		for i := 0; i < len(id); i++ {
			switch c := id[i]; {
			case c >= '0' && c <= '9':
			case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '-':
				numeric = false
			default:
				return false
			}
		}
		if numeric && len(id) > 1 && id[0] == '0' {
			return false
		}
	}
	return true
}

// numericID parses a semver numeric identifier: digits only, and no leading
// zero unless the whole identifier is "0".
func numericID(s string) (int, bool) {
	if s == "" || (len(s) > 1 && s[0] == '0') {
		return 0, false
	}
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
		n = n*10 + int(s[i]-'0')
	}
	return n, true
}

// Comparable reports whether v is a version this CLI can order. Development
// builds are not: `make build` passes no ldflags, so a local binary reports
// "dev".
func Comparable(v string) bool {
	_, ok := parse(v)
	return ok
}

// compare returns -1 if a sorts before b, 0 if they are equal, +1 if a sorts
// after b.
func compare(a, b parsed) int {
	for i := range a.nums {
		if a.nums[i] != b.nums[i] {
			return sign(a.nums[i] - b.nums[i])
		}
	}
	return comparePre(a.pre, b.pre)
}

// comparePre implements semver §11.3-11.4: a release outranks its own
// prereleases, and prerelease identifiers compare field by field.
func comparePre(a, b []string) int {
	switch {
	case len(a) == 0 && len(b) == 0:
		return 0
	case len(a) == 0:
		return 1
	case len(b) == 0:
		return -1
	}
	for i := 0; i < len(a) && i < len(b); i++ {
		if c := comparePreID(a[i], b[i]); c != 0 {
			return c
		}
	}
	// Every shared identifier matched: the smaller set precedes.
	return sign(len(a) - len(b))
}

func comparePreID(a, b string) int {
	na, aNum := numericID(a)
	nb, bNum := numericID(b)
	switch {
	case aNum && bNum:
		return sign(na - nb)
	case aNum:
		return -1 // numeric identifiers rank below alphanumeric
	case bNum:
		return 1
	default:
		return strings.Compare(a, b)
	}
}

// Newer reports whether latest is a strictly newer version than current. It is
// false whenever either side cannot be ordered, so a development build never
// triggers an upgrade.
//
// Parsing and ordering happen together: validating with Comparable and then
// comparing would parse both versions twice for the same answer.
func Newer(latest, current string) bool {
	l, lok := parse(latest)
	c, cok := parse(current)
	return lok && cok && compare(l, c) > 0
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	default:
		return 0
	}
}

package update

import "testing"

func TestComparable(t *testing.T) {
	cases := []struct {
		version string
		want    bool
	}{
		// Real shapes this CLI sees.
		{"0.7.0", true},  // ldflag injects {{.Version}} without the v
		{"v0.7.0", true}, // git tag and the releases/latest redirect carry it
		{"0.7.0-rc.1", true},
		{"v1.0.0-beta.2", true},
		{"1.2.3+meta", true},

		// Development builds must never be ordered: `make build` passes no
		// ldflags, so a local binary reports "dev".
		{"dev", false},
		{"", false},
		{"v", false},

		// Malformed input must not be silently treated as a version, or the
		// CLI would announce a bogus update.
		{"0.7", false},
		{"0.7.0.1", false},
		{"abc", false},
		{"0.x.0", false},
		{"v0.7.0-", false},
		{"-1.0.0", false},
	}
	for _, c := range cases {
		if got := Comparable(c.version); got != c.want {
			t.Errorf("Comparable(%q) = %v, want %v", c.version, got, c.want)
		}
	}
}

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		// The v prefix is cosmetic: 0.7.0 from the ldflag must equal v0.7.0
		// from the tag, or every release would look like an update.
		{"0.7.0", "v0.7.0", 0},
		{"v0.7.0", "0.7.0", 0},
		{"1.2.3+meta", "1.2.3", 0},

		{"0.7.0", "0.8.0", -1},
		{"0.8.0", "0.7.0", 1},
		{"0.7.0", "1.0.0", -1},
		{"0.7.0", "0.7.1", -1},
		{"0.10.0", "0.9.0", 1}, // numeric, not lexicographic
		{"0.7.10", "0.7.9", 1},

		// A prerelease precedes its own release.
		{"1.0.0-rc.1", "1.0.0", -1},
		{"1.0.0", "1.0.0-rc.1", 1},
		{"1.0.0-rc.1", "1.0.0-rc.2", -1},
		{"1.0.0-rc.2", "1.0.0-rc.10", -1}, // numeric identifiers compare numerically
		{"1.0.0-alpha", "1.0.0-beta", -1},
		{"1.0.0-alpha", "1.0.0-alpha.1", -1}, // shorter set precedes
		{"1.0.0-alpha.1", "1.0.0-alpha", 1},
		{"1.0.0-1", "1.0.0-alpha", -1}, // numeric precedes alphanumeric
		{"1.0.0-alpha", "1.0.0-1", 1},
		{"1.0.0-rc.1", "1.0.0-rc.1", 0},
	}
	for _, c := range cases {
		pa, aok := parse(c.a)
		pb, bok := parse(c.b)
		if !aok || !bok {
			t.Fatalf("fixture is not parseable: %q / %q", c.a, c.b)
		}
		if got := compare(pa, pb); got != c.want {
			t.Errorf("compare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

// Newer is the predicate the callers actually use; it must refuse to answer
// when either side is not a real version, so a dev build never triggers an
// upgrade.
func TestNewer(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"0.8.0", "0.7.0", true},
		{"v0.8.0", "0.7.0", true},
		{"0.7.0", "0.7.0", false},
		{"0.7.0", "0.8.0", false},

		{"0.8.0", "dev", false},
		{"dev", "0.7.0", false},
		{"", "0.7.0", false},
		{"0.8.0", "", false},
		{"garbage", "0.7.0", false},
	}
	for _, c := range cases {
		if got := Newer(c.latest, c.current); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.latest, c.current, got, c.want)
		}
	}
}

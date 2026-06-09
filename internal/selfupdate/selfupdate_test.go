package selfupdate

import (
	"testing"
	"time"
)

func TestSameCommit(t *testing.T) {
	full := "abc1234def5678abc1234def5678abc1234def56"
	cases := []struct {
		a, b string
		same bool
	}{
		{full, full, true},
		{"abc1234", full, true}, // short vs full
		{full, "abc1234", true},
		{"abc1234", "abc1235", false}, // differ within the prefix
		{"", full, false},
		{"ABC1234", "abc1234def", true}, // case-insensitive
	}
	for _, c := range cases {
		if got := sameCommit(c.a, c.b); got != c.same {
			t.Errorf("sameCommit(%q,%q) = %v, want %v", c.a, c.b, got, c.same)
		}
	}
}

func TestParseChecksum(t *testing.T) {
	h, err := parseChecksum([]byte("abcdef0123456789abcdef0123456789  prowl-agent-linux-amd64\n"))
	if err != nil || h != "abcdef0123456789abcdef0123456789" {
		t.Fatalf("parse = %q err=%v", h, err)
	}
	if _, err := parseChecksum([]byte("   ")); err == nil {
		t.Fatal("expected error on empty checksum")
	}
	if _, err := parseChecksum([]byte("short  x")); err == nil {
		t.Fatal("expected error on short digest")
	}
}

func TestCacheRoundTripAndTTL(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	writeCache(cache{CheckedAt: time.Now().Unix(), Available: true})
	c, ok := readCache()
	if !ok || !c.Available {
		t.Fatalf("fresh cache = %+v ok=%v", c, ok)
	}
	writeCache(cache{CheckedAt: time.Now().Add(-48 * time.Hour).Unix(), Available: true})
	if _, ok := readCache(); ok {
		t.Fatal("stale cache should be ignored")
	}
}

func TestCheckUsesCache(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	writeCache(cache{CheckedAt: time.Now().Unix(), Available: true})
	if r := Check("v9.9.9-deadbee"); !r.Available || !r.Checked {
		t.Fatalf("should honor cached availability without network: %+v", r)
	}
}

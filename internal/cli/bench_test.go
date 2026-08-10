package cli

import "testing"

func TestHumanInt(t *testing.T) {
	cases := map[int]string{0: "0", 42: "42", 999: "999", 1000: "1,000", 12345: "12,345", 338287: "338,287"}
	for in, want := range cases {
		if got := humanInt(in); got != want {
			t.Errorf("humanInt(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestClipStr(t *testing.T) {
	if got := clipStr("short", 40); got != "short" {
		t.Errorf("clipStr short = %q", got)
	}
	got := clipStr("this string is definitely longer than the cap", 10)
	if len([]rune(got)) != 10 {
		t.Errorf("clipStr len = %d, want 10 (%q)", len([]rune(got)), got)
	}
}

package cli

import (
	"context"
	"testing"
)

// fakeOllamaEnv records which side-effecting actions ensureOllamaRunning takes,
// so the decision order is tested without spawning ollama or systemctl.
func fakeOllamaEnv(rec *[]string) ollamaEnv {
	return ollamaEnv{
		reachable:      func(context.Context) bool { return false },
		hasUnit:        func() (bool, bool) { return false, false },
		systemdUser:    func() bool { return false },
		startUnit:      func(bool) error { *rec = append(*rec, "start"); return nil },
		writeAndEnable: func() error { *rec = append(*rec, "write"); return nil },
		spawnDetached:  func() error { *rec = append(*rec, "spawn"); return nil },
		warm:           func(context.Context) error { *rec = append(*rec, "warm"); return nil },
	}
}

func assertCallOrder(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("calls = %v, want %v", got, want)
		}
	}
}

func TestEnsureOllamaWarmsWhenReachable(t *testing.T) {
	var rec []string
	e := fakeOllamaEnv(&rec)
	e.reachable = func(context.Context) bool { return true }
	ensureOllamaRunning(context.Background(), e)
	assertCallOrder(t, rec, []string{"warm"})
}

func TestEnsureOllamaReusesExistingUnit(t *testing.T) {
	var rec []string
	reach := false
	e := fakeOllamaEnv(&rec)
	e.reachable = func(context.Context) bool { return reach }
	e.hasUnit = func() (bool, bool) { return false, true } // a system unit exists
	e.startUnit = func(bool) error { rec = append(rec, "start"); reach = true; return nil }
	ensureOllamaRunning(context.Background(), e)
	assertCallOrder(t, rec, []string{"start", "warm"})
}

func TestEnsureOllamaWritesUserUnitWhenSystemd(t *testing.T) {
	var rec []string
	reach := false
	e := fakeOllamaEnv(&rec)
	e.reachable = func(context.Context) bool { return reach }
	e.systemdUser = func() bool { return true }
	e.writeAndEnable = func() error { rec = append(rec, "write"); reach = true; return nil }
	ensureOllamaRunning(context.Background(), e)
	assertCallOrder(t, rec, []string{"write", "warm"})
}

func TestEnsureOllamaSpawnsWhenNoSystemd(t *testing.T) {
	var rec []string
	e := fakeOllamaEnv(&rec) // not reachable, no unit, no systemd
	ensureOllamaRunning(context.Background(), e)
	assertCallOrder(t, rec, []string{"spawn", "warm"})
}

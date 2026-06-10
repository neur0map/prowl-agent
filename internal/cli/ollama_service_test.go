package cli

import (
	"context"
	"errors"
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

func TestEnsureOllamaReachable(t *testing.T) {
	var rec []string
	e := fakeOllamaEnv(&rec)
	e.reachable = func(context.Context) bool { return true }
	if !ensureOllamaRunning(context.Background(), e) {
		t.Fatal("want reachable true")
	}
	assertCallOrder(t, rec, nil) // already up: takes no action
}

func TestEnsureOllamaReusesExistingUnit(t *testing.T) {
	var rec []string
	reach := false
	e := fakeOllamaEnv(&rec)
	e.reachable = func(context.Context) bool { return reach }
	e.hasUnit = func() (bool, bool) { return false, true } // a system unit exists
	e.startUnit = func(bool) error { rec = append(rec, "start"); reach = true; return nil }
	if !ensureOllamaRunning(context.Background(), e) {
		t.Fatal("want reachable true after start")
	}
	assertCallOrder(t, rec, []string{"start"})
}

func TestEnsureOllamaWritesUserUnitWhenSystemd(t *testing.T) {
	var rec []string
	reach := false
	e := fakeOllamaEnv(&rec)
	e.reachable = func(context.Context) bool { return reach }
	e.systemdUser = func() bool { return true }
	e.writeAndEnable = func() error { rec = append(rec, "write"); reach = true; return nil }
	if !ensureOllamaRunning(context.Background(), e) {
		t.Fatal("want reachable true after enabling the user unit")
	}
	assertCallOrder(t, rec, []string{"write"})
}

func TestEnsureOllamaSpawnsWhenNoSystemd(t *testing.T) {
	var rec []string
	reach := false
	e := fakeOllamaEnv(&rec)
	e.reachable = func(context.Context) bool { return reach }
	e.spawnDetached = func() error { rec = append(rec, "spawn"); reach = true; return nil }
	if !ensureOllamaRunning(context.Background(), e) {
		t.Fatal("want reachable true after spawn")
	}
	assertCallOrder(t, rec, []string{"spawn"})
}

func TestEnsureOllamaReturnsFalseWhenNothingWorks(t *testing.T) {
	var rec []string
	e := fakeOllamaEnv(&rec) // never reachable, no unit, no systemd, spawn does not help
	if ensureOllamaRunning(context.Background(), e) {
		t.Fatal("want reachable false when ollama cannot be started")
	}
	assertCallOrder(t, rec, []string{"spawn"})
}

var errFake = errors.New("fake failure")

func TestEnsureOllamaFallsBackToSpawnWhenStartFails(t *testing.T) {
	var rec []string
	reach := false
	e := fakeOllamaEnv(&rec)
	e.reachable = func(context.Context) bool { return reach }
	e.hasUnit = func() (bool, bool) { return false, true }
	e.startUnit = func(bool) error { rec = append(rec, "start"); return errFake }
	e.spawnDetached = func() error { rec = append(rec, "spawn"); reach = true; return nil }
	if !ensureOllamaRunning(context.Background(), e) {
		t.Fatal("want reachable true after spawn fallback")
	}
	assertCallOrder(t, rec, []string{"start", "spawn"})
}

func TestEnsureOllamaFallsBackToSpawnWhenEnableFails(t *testing.T) {
	var rec []string
	reach := false
	e := fakeOllamaEnv(&rec)
	e.reachable = func(context.Context) bool { return reach }
	e.systemdUser = func() bool { return true }
	e.writeAndEnable = func() error { rec = append(rec, "write"); return errFake }
	e.spawnDetached = func() error { rec = append(rec, "spawn"); reach = true; return nil }
	if !ensureOllamaRunning(context.Background(), e) {
		t.Fatal("want reachable true after spawn fallback")
	}
	assertCallOrder(t, rec, []string{"write", "spawn"})
}

func TestEnsureOllamaStartsUserUnitWithUserTrue(t *testing.T) {
	var rec []string
	var gotUser bool
	reach := false
	e := fakeOllamaEnv(&rec)
	e.reachable = func(context.Context) bool { return reach }
	e.hasUnit = func() (bool, bool) { return true, true }
	e.startUnit = func(user bool) error { gotUser = user; rec = append(rec, "start"); reach = true; return nil }
	if !ensureOllamaRunning(context.Background(), e) {
		t.Fatal("want reachable true")
	}
	if !gotUser {
		t.Fatal("startUnit should receive user=true for a user unit")
	}
	assertCallOrder(t, rec, []string{"start"})
}

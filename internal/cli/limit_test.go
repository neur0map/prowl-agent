package cli

import "testing"

func TestCapSlice(t *testing.T) {
	xs := []int{1, 2, 3, 4, 5}

	// limit <= 0 means no cap.
	if got := capSlice(xs, 0).([]int); len(got) != 5 {
		t.Errorf("limit 0 = %d, want 5 (no cap)", len(got))
	}
	if got := capSlice(xs, -1).([]int); len(got) != 5 {
		t.Errorf("limit -1 = %d, want 5 (no cap)", len(got))
	}
	// limit smaller than length truncates.
	if got := capSlice(xs, 3).([]int); len(got) != 3 || got[0] != 1 || got[2] != 3 {
		t.Errorf("limit 3 = %v, want first 3", got)
	}
	// limit >= length leaves it unchanged.
	if got := capSlice(xs, 10).([]int); len(got) != 5 {
		t.Errorf("limit 10 = %d, want 5", len(got))
	}
	// Non-slice values (struct results) pass through untouched.
	type box struct{ N int }
	if got := capSlice(box{N: 7}, 2).(box); got.N != 7 {
		t.Errorf("struct result was altered: %+v", got)
	}
}

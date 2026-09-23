package acp

import "testing"

func TestSessionListPositionCompare(t *testing.T) {
	newer := SessionListPosition{UpdatedAt: "2024-02-01T00:00:00Z", SessionID: "b"}
	older := SessionListPosition{UpdatedAt: "2024-01-01T00:00:00Z", SessionID: "a"}
	tie := SessionListPosition{UpdatedAt: newer.UpdatedAt, SessionID: "c"}
	undated := SessionListPosition{SessionID: "a"}
	ordered := []SessionListPosition{newer, tie, older, undated}
	for i := range ordered {
		for j := range ordered {
			got, want := ordered[i].Compare(ordered[j]), i-j
			if (got < 0) != (want < 0) || (got > 0) != (want > 0) {
				t.Errorf("%+v.Compare(%+v) = %d, want sign of %d", ordered[i], ordered[j], got, want)
			}
		}
	}
}

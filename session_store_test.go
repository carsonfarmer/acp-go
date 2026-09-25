package acp

import "testing"

func TestSessionListPositionCompare(t *testing.T) {
	newer := SessionListPosition{UpdatedAt: "2024-02-01T00:00:00Z", SessionID: "b"}
	older := SessionListPosition{UpdatedAt: "2024-01-01T00:00:00Z", SessionID: "a"}
	tie := SessionListPosition{UpdatedAt: newer.UpdatedAt, SessionID: "c"}
	undated := SessionListPosition{SessionID: "a"}
	checkOrder(t, newer, tie, older, undated)
}

// updatedAt compares as a time, since neither the width of two RFC 3339
// timestamps nor their offsets say which is newer: time.RFC3339Nano drops
// trailing zeros, so "10:00:00Z" is the longer string's prefix.
func TestSessionListPositionComparesTimes(t *testing.T) {
	checkOrder(t,
		SessionListPosition{UpdatedAt: "2026-09-25T19:00:01+09:00", SessionID: "a"},
		SessionListPosition{UpdatedAt: "2026-09-25T10:00:00.5Z", SessionID: "b"},
		SessionListPosition{UpdatedAt: "2026-09-25T10:00:00Z", SessionID: "a"},
		SessionListPosition{UpdatedAt: "2026-09-25T19:00:00+09:00", SessionID: "b"}, // the same time: the id decides
		SessionListPosition{UpdatedAt: "yesterday", SessionID: "a"},                 // not a time: after every time
		SessionListPosition{SessionID: "a"},
	)
}

// checkOrder checks that Compare orders every pair of positions as listed.
func checkOrder(t *testing.T, ordered ...SessionListPosition) {
	t.Helper()
	for i := range ordered {
		for j := range ordered {
			got, want := ordered[i].Compare(ordered[j]), i-j
			if (got < 0) != (want < 0) || (got > 0) != (want > 0) {
				t.Errorf("%+v.Compare(%+v) = %d, want sign of %d", ordered[i], ordered[j], got, want)
			}
		}
	}
}

func TestMemoryStoreZeroValue(t *testing.T) {
	var s MemoryStore[string, int]
	if _, ok, err := s.Get(t.Context(), "a"); ok || err != nil {
		t.Fatalf("empty store Get = %v, %v", ok, err)
	}
	if err := s.Set(t.Context(), "a", 1); err != nil {
		t.Fatal(err)
	}
	if got, ok, _ := s.Get(t.Context(), "a"); !ok || got != 1 {
		t.Fatalf("Get after Set = %d, %v", got, ok)
	}
	if ids, _ := s.List(t.Context()); len(ids) != 1 {
		t.Fatalf("List = %v", ids)
	}
}

package acpconn

import (
	"encoding/base64"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/ironpark/acp-go/internal/jsonrpc"
)

// pageOf streams items through a [SessionPager] and returns its page.
func pageOf[T any](items []T, cursor string, size int, key func(T) SessionPosition) ([]T, string, error) {
	pager, err := NewSessionPager(cursor, size, key)
	if err != nil {
		return nil, "", err
	}
	for _, item := range items {
		pager.Add(item)
	}
	page, next := pager.Page()
	return page, next, nil
}

func isInvalidParams(err error) bool {
	var reqErr *jsonrpc.RequestError
	return errors.As(err, &reqErr) && reqErr.Code == jsonrpc.CodeInvalidParams
}

func TestSessionCursorRoundTrip(t *testing.T) {
	want := SessionPosition{UpdatedAt: "2024-01-02T00:00:00Z", SessionID: "s1"}
	got, err := decodeSessionCursor(encodeSessionCursor(want))
	if err != nil || got != want {
		t.Fatalf("round trip = %+v, %v", got, err)
	}
	// A cursor without a timestamp is valid: undated sessions sort last.
	undated := SessionPosition{SessionID: "s2"}
	if got, err := decodeSessionCursor(encodeSessionCursor(undated)); err != nil || got != undated {
		t.Fatalf("undated round trip = %+v, %v", got, err)
	}
}

func TestDecodeSessionCursorRejectsGarbage(t *testing.T) {
	cases := []string{
		"not base64!!",
		base64.RawURLEncoding.EncodeToString([]byte("not json")),
	}
	for _, token := range cases {
		_, err := decodeSessionCursor(token)
		if !isInvalidParams(err) {
			t.Errorf("decodeSessionCursor(%q) = %v, want invalid params", token, err)
		}
	}
}

func TestSessionPager(t *testing.T) {
	type item struct{ id, updated string }
	items := []item{{"s3", "2023"}, {"s2", "2024"}, {"s1", "2024"}}
	key := func(i item) SessionPosition { return SessionPosition{UpdatedAt: i.updated, SessionID: i.id} }

	page, next, err := pageOf(items, "", 2, key)
	if err != nil || len(page) != 2 || page[0].id != "s1" || page[1].id != "s2" {
		t.Fatalf("first page = %+v, next=%q, err=%v", page, next, err)
	}
	if next == "" {
		t.Fatal("first page should hand back a cursor")
	}

	page, next, err = pageOf(items, next, 2, key)
	if err != nil || len(page) != 1 || page[0].id != "s3" || next != "" {
		t.Fatalf("second page = %+v, next=%q, err=%v", page, next, err)
	}
}

func TestSessionPagerNoLimitReturnsEverything(t *testing.T) {
	items := []string{"a", "b", "c"}
	key := func(s string) SessionPosition { return SessionPosition{SessionID: s} }
	page, next, err := pageOf(items, "", 0, key)
	if err != nil || len(page) != 3 || next != "" {
		t.Fatalf("unlimited page = %v, next=%q, err=%v", page, next, err)
	}
}

func TestSessionPagerExactFitHasNoCursor(t *testing.T) {
	items := []string{"a", "b"}
	key := func(s string) SessionPosition { return SessionPosition{SessionID: s} }
	page, next, err := pageOf(items, "", 2, key)
	if err != nil || len(page) != 2 || next != "" {
		t.Fatalf("exact-fit page = %v, next=%q, err=%v", page, next, err)
	}
}

func TestSessionPagerRejectsBadCursor(t *testing.T) {
	key := func(s string) SessionPosition { return SessionPosition{SessionID: s} }
	if _, _, err := pageOf([]string{"a"}, "!!!", 10, key); !isInvalidParams(err) {
		t.Fatalf("bad cursor = %v, want invalid params", err)
	}
}

// Paging through shuffled sessions with a small page size visits every session
// exactly once, in full-sort order, however the heap evicts candidates.
func TestSessionPagerMatchesFullSort(t *testing.T) {
	var items []SessionPosition
	for i := range 200 {
		items = append(items, SessionPosition{UpdatedAt: fmt.Sprint(i % 7), SessionID: fmt.Sprintf("s%03d", i)})
	}
	items = append(items, SessionPosition{SessionID: "undated"})
	want := slices.Clone(items)
	slices.SortFunc(want, SessionPosition.Compare)

	key := func(p SessionPosition) SessionPosition { return p }
	var got []SessionPosition
	cursor := ""
	for pages := 0; ; pages++ {
		if pages > len(items) {
			t.Fatal("paging did not terminate")
		}
		rand.Shuffle(len(items), func(i, j int) { items[i], items[j] = items[j], items[i] })
		page, next, err := pageOf(items, cursor, 9, key)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, page...)
		if next == "" {
			break
		}
		cursor = next
	}
	if !slices.Equal(got, want) {
		t.Fatalf("paged order differs from full sort:\n got %v\nwant %v", got, want)
	}
}

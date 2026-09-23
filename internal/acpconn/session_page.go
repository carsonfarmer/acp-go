package acpconn

import (
	"encoding/base64"
	"encoding/json/v2"
	"slices"
	"strings"

	"github.com/ironpark/acp-go/internal/jsonrpc"
)

// SessionPosition is a session's place in a session/list result: the sort key
// both façades' session managers order by. Clients see it only as the opaque
// cursor token [SessionPager] hands back.
type SessionPosition struct {
	// UpdatedAt is the session's last-activity timestamp, the primary sort
	// key. A session without one sorts after every dated session.
	UpdatedAt string `json:"u,omitzero"`
	// SessionID breaks ties between sessions with the same UpdatedAt.
	SessionID string `json:"i"`
}

// Compare orders two positions the way session/list orders sessions: the
// newest UpdatedAt first, then session id ascending. A positive result means p
// sorts after other, so other is on an earlier page.
func (p SessionPosition) Compare(other SessionPosition) int {
	if p.UpdatedAt != other.UpdatedAt {
		return strings.Compare(other.UpdatedAt, p.UpdatedAt)
	}
	return strings.Compare(p.SessionID, other.SessionID)
}

// encodeSessionCursor encodes a position as the opaque token a session/list
// request or response carries.
func encodeSessionCursor(p SessionPosition) string {
	data, err := json.Marshal(p)
	if err != nil {
		// A position is two strings; encoding cannot fail.
		panic("acpconn: encode session list cursor: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(data)
}

// decodeSessionCursor decodes a token from a session/list request. A malformed
// token becomes an invalid-params error, ready to return to the client.
func decodeSessionCursor(token string) (SessionPosition, error) {
	var p SessionPosition
	data, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return p, jsonrpc.InvalidParams(nil, "invalid cursor")
	}
	if err := json.Unmarshal(data, &p); err != nil {
		return p, jsonrpc.InvalidParams(nil, "invalid cursor")
	}
	return p, nil
}

// SessionPager collects one page of a session/list result while the caller
// streams every session through [SessionPager.Add], so a page costs memory for
// the page alone and never sorts the whole result.
//
// Sessions are ordered by their key position, newest UpdatedAt first and then
// session id ascending. The page skips every session at or before the request
// cursor and takes up to size sessions; a size of zero or less takes every
// remaining session.
type SessionPager[T any] struct {
	key   func(T) SessionPosition
	after *SessionPosition
	size  int
	// kept holds the best candidates, up to size+1 of them, as a heap with
	// the one that sorts last on top, so the next better session evicts it.
	kept []T
}

// NewSessionPager starts a page after the request's cursor token, or at the
// first session when cursor is "". A malformed cursor is returned as an
// invalid-params error before the caller reads any session.
func NewSessionPager[T any](cursor string, size int, key func(T) SessionPosition) (*SessionPager[T], error) {
	p := &SessionPager[T]{key: key, size: size}
	if cursor != "" {
		after, err := decodeSessionCursor(cursor)
		if err != nil {
			return nil, err
		}
		p.after = &after
	}
	return p, nil
}

// After is the position the page starts after, or nil for the first page.
func (p *SessionPager[T]) After() *SessionPosition {
	if p.after == nil {
		return nil
	}
	after := *p.after
	return &after
}

// Limit is how many sessions, in order after [SessionPager.After], the pager
// needs to fill the page and tell whether another follows: one more than the
// page size, or zero when the page takes every session.
func (p *SessionPager[T]) Limit() int {
	if p.size <= 0 {
		return 0
	}
	return p.size + 1
}

// Add offers one session for the page.
func (p *SessionPager[T]) Add(item T) {
	pos := p.key(item)
	if p.after != nil && pos.Compare(*p.after) <= 0 {
		return // on an earlier page
	}
	if p.size <= 0 {
		p.kept = append(p.kept, item)
		return
	}
	// One extra candidate tells Page whether another page follows.
	if len(p.kept) <= p.size {
		p.kept = append(p.kept, item)
		p.up(len(p.kept) - 1)
		return
	}
	if pos.Compare(p.key(p.kept[0])) < 0 {
		p.kept[0] = item
		p.down(0)
	}
}

// Page returns the page in session/list order and the token to pass as the
// request cursor for the following page, or "" when the page is the last.
func (p *SessionPager[T]) Page() (page []T, next string) {
	page = p.kept
	slices.SortFunc(page, func(a, b T) int { return p.key(a).Compare(p.key(b)) })
	if p.size > 0 && len(page) > p.size {
		page = page[:p.size]
		next = encodeSessionCursor(p.key(page[len(page)-1]))
	}
	return page, next
}

// later reports whether kept[i] sorts after kept[j], the heap's order.
func (p *SessionPager[T]) later(i, j int) bool {
	return p.key(p.kept[i]).Compare(p.key(p.kept[j])) > 0
}

func (p *SessionPager[T]) up(i int) {
	for i > 0 {
		parent := (i - 1) / 2
		if !p.later(i, parent) {
			return
		}
		p.kept[i], p.kept[parent] = p.kept[parent], p.kept[i]
		i = parent
	}
}

func (p *SessionPager[T]) down(i int) {
	for {
		top, l, r := i, 2*i+1, 2*i+2
		if l < len(p.kept) && p.later(l, top) {
			top = l
		}
		if r < len(p.kept) && p.later(r, top) {
			top = r
		}
		if top == i {
			return
		}
		p.kept[i], p.kept[top] = p.kept[top], p.kept[i]
		i = top
	}
}

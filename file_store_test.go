package acp

import (
	"context"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

// fileSession is session state with unexported fields and a lock, as agents
// write it, saved through its own JSON methods.
type fileSession struct {
	mu   sync.Mutex
	cwd  string
	mode string
}

func (s *fileSession) MarshalJSON() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return json.Marshal(map[string]string{"cwd": s.cwd, "mode": s.mode})
}

func (s *fileSession) UnmarshalJSON(data []byte) error {
	var fields map[string]string
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	s.cwd, s.mode = fields["cwd"], fields["mode"]
	return nil
}

func openFileStore(t *testing.T, dir string) *FileStore[string, *fileSession] {
	t.Helper()
	store, err := NewFileStore[string, *fileSession](dir)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestFileStoreSurvivesRestart(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	store := openFileStore(t, dir)
	// Ids that are not safe as file names still get their own file.
	ids := []string{"session_1", "../escape", "a/b:c", "A", "a"}
	for _, id := range ids {
		if err := store.Set(ctx, id, &fileSession{cwd: "/w/" + id, mode: "ask"}); err != nil {
			t.Fatal(err)
		}
	}
	// A change in place reaches disk only when the session is set again.
	session, _, _ := store.Get(ctx, "session_1")
	session.mode = "code"
	if got, _, _ := store.Get(ctx, "session_1"); got != session {
		t.Fatal("Get should return the same value every time")
	}
	if err := store.Set(ctx, "session_1", session); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(ctx, "A"); err != nil {
		t.Fatal(err)
	}

	reopened := openFileStore(t, dir)
	got, err := reopened.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(got)
	if want := []string{"../escape", "a", "a/b:c", "session_1"}; !slices.Equal(got, want) {
		t.Fatalf("reopened ids = %q, want %q", got, want)
	}
	for _, id := range got {
		s, ok, err := reopened.Get(ctx, id)
		if err != nil || !ok || s.cwd != "/w/"+id {
			t.Fatalf("Get(%q) = %+v, %v, %v", id, s, ok, err)
		}
	}
	if s, _, _ := reopened.Get(ctx, "session_1"); s.mode != "code" {
		t.Fatalf("saved mode = %q, want code", s.mode)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 4 {
		t.Fatalf("directory holds %d files, want 4", len(entries))
	}
}

func TestFileStoreDeleteUnknownSucceeds(t *testing.T) {
	if err := openFileStore(t, t.TempDir()).Delete(t.Context(), "missing"); err != nil {
		t.Fatal(err)
	}
}

func TestFileStoreRemovesInterruptedWrites(t *testing.T) {
	dir := t.TempDir()
	leftover := filepath.Join(dir, fileStoreTemp+"123")
	if err := os.WriteFile(leftover, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := openFileStore(t, dir)
	if ids, _ := store.List(t.Context()); len(ids) != 0 {
		t.Fatalf("ids = %q, want none", ids)
	}
	if _, err := os.Stat(leftover); !os.IsNotExist(err) {
		t.Fatalf("leftover temporary file still exists: %v", err)
	}
}

func TestFileStoreRejectsCorruptFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "73.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFileStore[string, *fileSession](dir); err == nil || !strings.Contains(err.Error(), "73.json") {
		t.Fatalf("NewFileStore = %v, want an error naming the corrupt file", err)
	}
}

func TestFileStoreKeepsPreviousVersionWhenEncodingFails(t *testing.T) {
	ctx := context.Background()
	store, err := NewFileStore[string, any](t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set(ctx, "s", "first"); err != nil {
		t.Fatal(err)
	}
	if err := store.Set(ctx, "s", func() {}); err == nil {
		t.Fatal("Set of an unencodable session should fail")
	}
	if got, _, _ := store.Get(ctx, "s"); got != "first" {
		t.Fatalf("Get = %v, want the previous version", got)
	}
}

func TestFileStoreConcurrentUse(t *testing.T) {
	ctx := t.Context()
	store := openFileStore(t, t.TempDir())
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			id := string(rune('a' + i))
			for range 20 {
				if err := store.Set(ctx, id, &fileSession{cwd: "/" + id}); err != nil {
					t.Error(err)
					return
				}
				_, _, _ = store.Get(ctx, id)
				_, _ = store.List(ctx)
			}
		})
	}
	wg.Wait()
	if ids, _ := store.List(ctx); len(ids) != 8 {
		t.Fatalf("ids = %q, want 8", ids)
	}
}

func TestFileStoreCreatesMissingDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state", "sessions")
	store := openFileStore(t, dir)
	if err := store.Set(t.Context(), "s", &fileSession{cwd: "/"}); err != nil {
		t.Fatal(err)
	}
	if ids, _ := openFileStore(t, dir).List(t.Context()); len(ids) != 1 {
		t.Fatalf("ids = %q, want the saved session", ids)
	}
}

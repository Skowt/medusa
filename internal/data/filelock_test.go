package data

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Each writer gets its own Registry, so the in-process mutex cannot serialize
// them. flock is held per open file description on darwin and linux, so this
// is the same contention two medusa processes produce.
func TestRegistryConcurrentWritersLoseNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspaces.json")
	const writers, perWriter = 8, 20

	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			reg := NewRegistry(path)
			for i := 0; i < perWriter; i++ {
				id := fmt.Sprintf("w%d-%d", w, i)
				if err := reg.AddWorkspace(id, id, ""); err != nil {
					t.Errorf("AddWorkspace(%s): %v", id, err)
				}
			}
		}(w)
	}
	wg.Wait()

	entries, err := NewRegistry(path).ListWorkspaces()
	if err != nil {
		t.Fatalf("ListWorkspaces: %v", err)
	}
	if len(entries) != writers*perWriter {
		t.Fatalf("got %d entries, want %d: concurrent writers lost updates", len(entries), writers*perWriter)
	}
}

func TestRegistryReaderNeverSeesPartialFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspaces.json")
	writer := NewRegistry(path)
	reader := NewRegistry(path)
	if err := writer.AddWorkspace("seed", "seed", ""); err != nil {
		t.Fatal(err)
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			id := fmt.Sprintf("ws-%d", i)
			_ = writer.AddWorkspace(id, id, "")
			_ = writer.RemoveWorkspace(id)
		}
	}()

	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		if _, err := reader.ListWorkspaces(); err != nil {
			close(stop)
			wg.Wait()
			t.Fatalf("reader saw a broken registry: %v", err)
		}
	}
	close(stop)
	wg.Wait()
}

func TestRegistryHas(t *testing.T) {
	reg := NewRegistry(filepath.Join(t.TempDir(), "workspaces.json"))
	if ok, err := reg.Has("a"); err != nil || ok {
		t.Fatalf("Has on empty registry = %v, %v; want false, nil", ok, err)
	}
	if err := reg.AddWorkspace("a", "a", ""); err != nil {
		t.Fatal(err)
	}
	if ok, err := reg.Has("a"); err != nil || !ok {
		t.Fatalf("Has after add = %v, %v; want true, nil", ok, err)
	}
}

func TestRecentsWriteIsAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "recents.json")

	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			store := NewRecentsStore(path)
			for i := 0; i < 10; i++ {
				repo := RepoRef{Name: "r", Path: fmt.Sprintf("/repo/%d/%d", w, i)}
				if err := store.Add([]RepoRef{repo}); err != nil {
					t.Errorf("Add: %v", err)
				}
			}
		}(w)
	}
	wg.Wait()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var entries []RecentEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatalf("recents.json is not valid JSON after concurrent writes: %v", err)
	}
	if len(entries) != maxRecents {
		t.Fatalf("got %d recents, want %d", len(entries), maxRecents)
	}
	left, _ := os.ReadDir(dir)
	for _, e := range left {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}
}

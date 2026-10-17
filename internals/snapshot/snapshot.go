package snapshot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Stats struct {
	Files       int   `json:"files"`
	FilesReused int   `json:"files_reused"`
	BytesRead   int64 `json:"bytes_read"`
	NewChunks   int   `json:"new_chunks"`
	NewBytes    int64 `json:"new_bytes"`
	DedupChunks int   `json:"dedup_chunks"`
}

type Snapshot struct {
	ID          string   `json:"id"`
	RootTree    string   `json:"root_tree"`
	Host        string   `json:"host"`
	Paths       []string `json:"paths"`
	ParentID    string   `json:"parent_id,omitempty"`
	StartedAt   int64    `json:"started_at_ns"`
	CommittedAt int64    `json:"committed_at_ns"`
	Stats       Stats    `json:"stats"`
}

type Catalog struct{ dir string }

func NewCatalog(repo string) (*Catalog, error) {
	d := filepath.Join(repo, "snapshots")
	if err := os.MkdirAll(d, 0o755); err != nil {
		return nil, err
	}
	return &Catalog{dir: d}, nil
}

// Commit makes the snapshot visible atomically: temp file, fsync, rename.
// Before the rename the snapshot does not exist (FR-6).
func (c *Catalog) Commit(s *Snapshot) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(c.dir, ".tmp-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(c.dir, s.ID+".json"))
}

func (c *Catalog) List() ([]*Snapshot, error) {
	ents, err := os.ReadDir(c.dir)
	if err != nil {
		return nil, err
	}
	var out []*Snapshot
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(c.dir, e.Name()))
		if err != nil {
			return nil, err
		}
		var s Snapshot
		if err := json.Unmarshal(b, &s); err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		out = append(out, &s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CommittedAt < out[j].CommittedAt })
	return out, nil
}

// Get accepts a full ID, an ID prefix, or "latest".
func (c *Catalog) Get(id string) (*Snapshot, error) {
	all, err := c.List()
	if err != nil {
		return nil, err
	}
	if len(all) == 0 {
		return nil, fmt.Errorf("no snapshots in repository")
	}
	if id == "latest" {
		return all[len(all)-1], nil
	}
	if id != "" {
		for _, s := range all {
			if strings.HasPrefix(s.ID, id) {
				return s, nil
			}
		}
	}
	return nil, fmt.Errorf("snapshot %q not found", id)
}

// LatestForPath returns the newest snapshot of this absolute path (the parent), or nil.
func (c *Catalog) LatestForPath(abs string) (*Snapshot, error) {
	all, err := c.List()
	if err != nil {
		return nil, err
	}
	for i := len(all) - 1; i >= 0; i-- {
		if len(all[i].Paths) > 0 && all[i].Paths[0] == abs {
			return all[i], nil
		}
	}
	return nil, nil
}

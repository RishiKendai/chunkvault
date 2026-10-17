package tree

import (
	"encoding/json"
	"sort"
)

const (
	TypeFile    = "file"
	TypeDir     = "dir"
	TypeSymlink = "symlink"
)

type Entry struct {
	Name       string   `json:"name"`
	Type       string   `json:"type"`
	Mode       uint32   `json:"mode"`
	Size       int64    `json:"size"`
	MtimeNs    int64    `json:"mtime_ns"`
	Inode      uint64   `json:"inode"`
	Chunks     []string `json:"chunks,omitempty"`
	Subtree    string   `json:"subtree,omitempty"`
	LinkTarget string   `json:"link_target,omitempty"`
}

type Tree struct {
	Entries []Entry `json:"entries"`
}

// Encode is deterministic: entries sorted by name, fixed field order, no maps.
// Determinism is what lets identical directories dedup.
func (t *Tree) Encode() ([]byte, error) {
	sort.Slice(t.Entries, func(i, j int) bool { return t.Entries[i].Name < t.Entries[j].Name })
	return json.Marshal(t)
}

func Decode(b []byte) (*Tree, error) {
	var t Tree
	if err := json.Unmarshal(b, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (t *Tree) Find(name string) *Entry {
	for i := range t.Entries {
		if t.Entries[i].Name == name {
			return &t.Entries[i]
		}
	}
	return nil
}

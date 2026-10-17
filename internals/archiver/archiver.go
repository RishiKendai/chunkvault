package archiver

import (
	crand "crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/RishiKendai/chunkvault/internals/chunker"
	"github.com/RishiKendai/chunkvault/internals/crypto"
	"github.com/RishiKendai/chunkvault/internals/snapshot"
	"github.com/RishiKendai/chunkvault/internals/store"
	"github.com/RishiKendai/chunkvault/internals/tree"
	"github.com/RishiKendai/chunkvault/internals/types"
)

type Archiver struct {
	Store   *store.Local
	IDer    crypto.IDer
	Params  chunker.Params
	Exclude string // absolute path to skip (the repo itself, if it sits inside the source)
	stats   snapshot.Stats
}

func newID() string {
	var r [4]byte
	_, _ = crand.Read(r[:])
	return time.Now().UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(r[:])
}

func inodeOf(fi fs.FileInfo) uint64 {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Ino)
	}
	return 0
}

// Run builds a snapshot of src. It does NOT commit it; the caller does (atomic visibility).
func (a *Archiver) Run(src string, parent *snapshot.Snapshot) (*snapshot.Snapshot, error) {
	a.stats = snapshot.Stats{}
	started := time.Now()

	var parentTree *tree.Tree
	if parent != nil {
		pt, err := a.loadTree(parent.RootTree)
		if err == nil {
			parentTree = pt // if unreadable, just do a full scan
		}
	}

	fi, err := os.Lstat(src)
	if err != nil {
		return nil, err
	}
	var rootID string
	if fi.IsDir() {
		rootID, err = a.archiveDir(src, parentTree)
		if err != nil {
			return nil, err
		}
	} else {
		var pe *tree.Entry
		if parentTree != nil {
			pe = parentTree.Find(fi.Name())
		}
		ent, ok, err := a.archiveEntry(src, fi, pe)
		if err != nil {
			return nil, err
		}
		t := &tree.Tree{Entries: []tree.Entry{}}
		if ok {
			t.Entries = append(t.Entries, ent)
		}
		rootID, err = a.putTree(t)
		if err != nil {
			return nil, err
		}
	}

	host, _ := os.Hostname()
	snap := &snapshot.Snapshot{
		ID: newID(), RootTree: rootID, Host: host, Paths: []string{src},
		StartedAt: started.UnixNano(), CommittedAt: time.Now().UnixNano(), Stats: a.stats,
	}
	if parent != nil {
		snap.ParentID = parent.ID
	}
	return snap, nil
}

func (a *Archiver) archiveDir(dir string, parent *tree.Tree) (string, error) {
	des, err := os.ReadDir(dir) // sorted by name
	if err != nil {
		return "", err
	}
	t := &tree.Tree{Entries: []tree.Entry{}}
	for _, de := range des {
		path := filepath.Join(dir, de.Name())
		if path == a.Exclude {
			continue
		}
		fi, err := os.Lstat(path)
		if err != nil {
			return "", err
		}
		var pe *tree.Entry
		if parent != nil {
			pe = parent.Find(de.Name())
		}
		ent, ok, err := a.archiveEntry(path, fi, pe)
		if err != nil {
			return "", err
		}
		if ok {
			t.Entries = append(t.Entries, ent)
		}
	}
	return a.putTree(t)
}

func (a *Archiver) archiveEntry(path string, fi fs.FileInfo, pe *tree.Entry) (tree.Entry, bool, error) {
	ent := tree.Entry{
		Name: fi.Name(), Mode: uint32(fi.Mode().Perm()), Size: fi.Size(),
		MtimeNs: fi.ModTime().UnixNano(), Inode: inodeOf(fi),
	}
	switch {
	case fi.Mode()&fs.ModeSymlink != 0:
		target, err := os.Readlink(path)
		if err != nil {
			return ent, false, err
		}
		ent.Type, ent.LinkTarget, ent.Size = tree.TypeSymlink, target, 0
		return ent, true, nil

	case fi.IsDir():
		var ptree *tree.Tree
		if pe != nil && pe.Type == tree.TypeDir {
			if pt, err := a.loadTree(pe.Subtree); err == nil {
				ptree = pt
			}
		}
		id, err := a.archiveDir(path, ptree)
		if err != nil {
			return ent, false, err
		}
		ent.Type, ent.Subtree, ent.Size = tree.TypeDir, id, 0
		return ent, true, nil

	case fi.Mode().IsRegular():
		ent.Type = tree.TypeFile
		a.stats.Files++
		// Change detection (FR-4): same size + mtime + inode as parent => reuse chunk list, no read.
		if pe != nil && pe.Type == tree.TypeFile && pe.Size == ent.Size &&
			pe.MtimeNs == ent.MtimeNs && pe.Inode == ent.Inode {
			ent.Chunks = pe.Chunks
			a.stats.FilesReused++
			return ent, true, nil
		}
		chunks, err := a.chunkFile(path)
		if err != nil {
			return ent, false, err
		}
		ent.Chunks = chunks
		return ent, true, nil

	default: // sockets, devices, pipes: skipped
		return ent, false, nil
	}
}

func (a *Archiver) chunkFile(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	ck, err := chunker.NewFastCDC(f, a.Params)
	if err != nil {
		return nil, err
	}
	var ids []string
	for {
		c, err := ck.Next()
		if err == io.EOF {
			return ids, nil
		}
		if err != nil {
			return nil, err
		}
		id, err := a.putBlob(c.Data)
		if err != nil {
			return nil, err
		}
		a.stats.BytesRead += int64(len(c.Data))
		ids = append(ids, id.String())
	}
}

// putBlob is the dedup point: skip the write if the content is already stored.
func (a *Archiver) putBlob(data []byte) (types.ChunkID, error) {
	id := a.IDer.ID(data)
	has, err := a.Store.Has(id)
	if err != nil {
		return id, err
	}
	if has {
		a.stats.DedupChunks++
		return id, nil
	}
	if err := a.Store.Put(id, data); err != nil {
		return id, err
	}
	a.stats.NewChunks++
	a.stats.NewBytes += int64(len(data))
	return id, nil
}

func (a *Archiver) putTree(t *tree.Tree) (string, error) {
	b, err := t.Encode()
	if err != nil {
		return "", err
	}
	id, err := a.putBlob(b)
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

func (a *Archiver) readChunk(h string) ([]byte, error) {
	id, err := types.ParseChunkID(h)
	if err != nil {
		return nil, err
	}
	data, err := a.Store.Get(id)
	if err != nil {
		return nil, fmt.Errorf("chunk %s: %w", h[:8], err)
	}
	if a.IDer.ID(data) != id { // end-to-end integrity check
		return nil, fmt.Errorf("chunk %s is corrupt", h[:8])
	}
	return data, nil
}

func (a *Archiver) loadTree(idHex string) (*tree.Tree, error) {
	data, err := a.readChunk(idHex)
	if err != nil {
		return nil, err
	}
	return tree.Decode(data)
}

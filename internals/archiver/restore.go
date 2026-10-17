package archiver

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/RishiKendai/chunkvault/internals/tree"
	"github.com/RishiKendai/chunkvault/internals/types"
)

func (a *Archiver) Restore(rootTree, target string) error {
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}
	return a.restoreDir(rootTree, target)
}

func (a *Archiver) restoreDir(treeID, dir string) error {
	t, err := a.loadTree(treeID)
	if err != nil {
		return fmt.Errorf("tree %s: %w", treeID[:8], err)
	}
	for _, e := range t.Entries {
		if filepath.Base(e.Name) != e.Name || e.Name == "." || e.Name == ".." {
			return fmt.Errorf("unsafe entry name %q", e.Name) // path traversal guard
		}
		p := filepath.Join(dir, e.Name)
		mt := time.Unix(0, e.MtimeNs)
		switch e.Type {
		case tree.TypeDir:
			if err := os.MkdirAll(p, 0o755); err != nil {
				return err
			}
			if err := a.restoreDir(e.Subtree, p); err != nil {
				return err
			}
			if err := os.Chmod(p, fs.FileMode(e.Mode)); err != nil {
				return err
			}
			if err := os.Chtimes(p, mt, mt); err != nil { // after children are written
				return err
			}
		case tree.TypeSymlink:
			_ = os.Remove(p)
			if err := os.Symlink(e.LinkTarget, p); err != nil {
				return err
			}
		case tree.TypeFile:
			if err := a.restoreFile(e, p); err != nil {
				return err
			}
		}
	}
	return nil
}

func (a *Archiver) restoreFile(e tree.Entry, p string) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(p), ".cv-tmp-*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}()
	var written int64
	for _, h := range e.Chunks {
		data, rerr := a.readChunk(h)
		if rerr != nil {
			return fmt.Errorf("%s: %w", e.Name, rerr)
		}
		n, werr := tmp.Write(data)
		if werr != nil {
			return werr
		}
		written += int64(n)
	}
	if written != e.Size {
		return fmt.Errorf("%s: restored %d bytes, expected %d", e.Name, written, e.Size)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), fs.FileMode(e.Mode)); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), p); err != nil {
		return err
	}
	mt := time.Unix(0, e.MtimeNs)
	return os.Chtimes(p, mt, mt) // set mtime after content is complete
}

// Verify walks a snapshot's trees and checks every chunk exists (and, with readData,
// re-hashes it). It returns problems; an empty slice means the snapshot is restorable.
func (a *Archiver) Verify(treeID string, readData bool) ([]string, error) {
	t, err := a.loadTree(treeID)
	if err != nil {
		return []string{fmt.Sprintf("tree %s: %v", treeID[:8], err)}, nil
	}
	var problems []string
	for _, e := range t.Entries {
		switch e.Type {
		case tree.TypeDir:
			p, err := a.Verify(e.Subtree, readData)
			if err != nil {
				return nil, err
			}
			problems = append(problems, p...)
		case tree.TypeFile:
			for _, h := range e.Chunks {
				if readData {
					if _, err := a.readChunk(h); err != nil {
						problems = append(problems, fmt.Sprintf("%s: %v", e.Name, err))
					}
					continue
				}
				id, err := types.ParseChunkID(h)
				if err != nil {
					problems = append(problems, fmt.Sprintf("%s: %v", e.Name, err))
					continue
				}
				ok, err := a.Store.Has(id)
				if err != nil {
					return nil, err
				}
				if !ok {
					problems = append(problems, fmt.Sprintf("%s: chunk %s missing", e.Name, h[:8]))
				}
			}
		}
	}
	return problems, nil
}

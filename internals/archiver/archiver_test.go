package archiver

import (
	"bytes"
	"crypto/rand"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/RishiKendai/chunkvault/internals/chunker"
	"github.com/RishiKendai/chunkvault/internals/crypto"
	"github.com/RishiKendai/chunkvault/internals/snapshot"

	// "github.com/RishiKendai/chunkvault/internals/store"
	"github.com/RishiKendai/chunkvault/internals/pack"
)

func newArch(t *testing.T) (*Archiver, *snapshot.Catalog) {
	t.Helper()
	repo := t.TempDir()
	st, err := pack.Open(repo)
	if err != nil {
		t.Fatal(err)
	}
	cat, err := snapshot.NewCatalog(repo)
	if err != nil {
		t.Fatal(err)
	}
	return &Archiver{Store: st, IDer: crypto.SHA256IDer{}, Params: chunker.DefaultParams, Exclude: repo}, cat
}

func write(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func sameTree(t *testing.T, src, dst string) {
	t.Helper()
	filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		rel, _ := filepath.Rel(src, p)
		if rel == "." {
			return nil
		}
		q := filepath.Join(dst, rel)
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			a, _ := os.Readlink(p)
			b, _ := os.Readlink(q)
			if a != b {
				t.Errorf("symlink %s: %q vs %q", rel, a, b)
			}
		case d.IsDir():
			if st, err := os.Stat(q); err != nil || !st.IsDir() {
				t.Errorf("dir %s missing", rel)
			}
		default:
			x, _ := os.ReadFile(p)
			y, err := os.ReadFile(q)
			if err != nil || !bytes.Equal(x, y) {
				t.Errorf("file %s differs", rel)
			}
		}
		return nil
	})
}

func TestRoundTripAndIncremental(t *testing.T) {
	a, cat := newArch(t)
	src := t.TempDir()
	big := make([]byte, 6<<20)
	rand.Read(big)
	write(t, filepath.Join(src, "big.bin"), big)
	write(t, filepath.Join(src, "sub/deep/a.txt"), []byte("hello"))
	write(t, filepath.Join(src, "empty.txt"), nil)
	if err := os.Symlink("a.txt", filepath.Join(src, "sub/deep/link")); err != nil {
		t.Fatal(err)
	}

	snap1, err := a.Run(src, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := cat.Commit(snap1); err != nil {
		t.Fatal(err)
	}

	dst := t.TempDir()
	if err := a.Restore(snap1.RootTree, dst); err != nil {
		t.Fatal(err)
	}
	sameTree(t, src, dst)

	if p, err := a.Verify(snap1.RootTree, true); err != nil || len(p) != 0 {
		t.Fatalf("verify: %v %v", p, err)
	}

	// Unchanged second backup: nothing read, nothing new, identical root tree.
	snap2, err := a.Run(src, snap1)
	if err != nil {
		t.Fatal(err)
	}
	if snap2.Stats.NewChunks != 0 || snap2.Stats.BytesRead != 0 {
		t.Fatalf("unchanged backup wrote/read data: %+v", snap2.Stats)
	}
	if snap2.Stats.FilesReused != snap2.Stats.Files {
		t.Fatalf("expected all files reused: %+v", snap2.Stats)
	}
	if snap2.RootTree != snap1.RootTree {
		t.Fatal("unchanged tree must have an identical root (determinism)")
	}

	// Change one file: only its changed chunk(s) plus the root tree are new.
	write(t, filepath.Join(src, "big.bin"), append(big, 0x42))
	snap3, err := a.Run(src, snap2)
	if err != nil {
		t.Fatal(err)
	}
	if snap3.Stats.NewChunks > 5 {
		t.Fatalf("too many new chunks for a 1-byte append: %+v", snap3.Stats)
	}
}

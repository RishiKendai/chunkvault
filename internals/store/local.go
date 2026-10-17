package store

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/RishiKendai/chunkvault/internals/types"
)

const OBJECT = "objects"

type Local struct{ root string }

func NewLocal(root string) (*Local, error) {
	if err := os.MkdirAll(filepath.Join(root, OBJECT), 0o755); err != nil {
		return nil, err
	}
	return &Local{root: root}, nil
}

func (s *Local) path(id types.ChunkID) string {
	h := id.String()
	return filepath.Join(s.root, OBJECT, h[:2], h[2:])
}

func (s *Local) Has(id types.ChunkID) (bool, error) {
	_, err := os.Stat(s.path(id))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return false, err
}

// Put writes via temp file + rename so a partial chunk never appears under its real name
func (s *Local) Put(id types.ChunkID, data []byte) error {
	p := s.path(id)
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, "temp-*")
	if err != nil {
		return err
	}
	cleanup := func() { tmp.Close(); os.Remove(tmp.Name()) }
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return err
	}

	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}

	return os.Rename(tmp.Name(), p)
}

func (s *Local) Get(id types.ChunkID) ([]byte, error) {
	return os.ReadFile(s.path(id))
}

func (s *Local) Root() string { return s.root }

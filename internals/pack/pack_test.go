package pack

import (
	"bytes"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/RishiKendai/chunkvault/internals/types"
)

type item struct {
	id types.ChunkID
	d  []byte
}

func mk(n int) item {
	d := make([]byte, n)
	rand.Read(d)
	var id types.ChunkID
	rand.Read(id[:])
	return item{id, d}
}

func TestRoundTripReopen(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	var items []item
	for i := 0; i < 5; i++ {
		it := mk(1000 + i)
		items = append(items, it)
		if err := s.Put(it.id, it.d); err != nil {
			t.Fatal(err)
		}
	}
	s.Put(items[0].id, items[0].d) // duplicate is a no-op
	s.Sync()
	s.Close()

	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s2.Count() != 5 {
		t.Fatalf("count = %d", s2.Count())
	}
	for _, it := range items {
		got, err := s2.Get(it.id)
		if err != nil || !bytes.Equal(got, it.d) {
			t.Fatalf("get failed: %v", err)
		}
	}
}

func TestRotation(t *testing.T) {
	dir := t.TempDir()
	s, _ := OpenSize(dir, 4096)
	var items []item
	for i := 0; i < 20; i++ {
		it := mk(1000)
		items = append(items, it)
		if err := s.Put(it.id, it.d); err != nil {
			t.Fatal(err)
		}
	}
	s.Sync()
	s.Close()
	packs, _ := filepath.Glob(filepath.Join(dir, "packs", "*.pack"))
	if len(packs) < 4 {
		t.Fatalf("expected several packs, got %d", len(packs))
	}
	s2, err := OpenSize(dir, 4096)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if got, err := s2.Get(it.id); err != nil || !bytes.Equal(got, it.d) {
			t.Fatalf("get after reopen failed: %v", err)
		}
	}
}

// Truncate the last record at EVERY byte offset: recovery must never fail or return garbage.
func TestTornTailEveryOffset(t *testing.T) {
	const payload = 100
	rec := int64(48 + payload) // magic 4 + len 4 + id 32 + gen 4 + payload + crc 4
	lastStart := int64(headerSize) + 2*rec
	full := lastStart + rec
	items := []item{mk(payload), mk(payload), mk(payload)}

	for cut := lastStart; cut < full; cut++ {
		dir := t.TempDir()
		s, err := Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, it := range items {
			s.Put(it.id, it.d)
		}
		s.Sync()
		s.Close()
		path := filepath.Join(dir, "packs", "00000001.pack")
		if st, _ := os.Stat(path); st.Size() != full {
			t.Fatalf("unexpected pack size %d", st.Size())
		}
		os.Truncate(path, cut)

		s2, err := Open(dir)
		if err != nil {
			t.Fatalf("cut %d: %v", cut, err)
		}
		for i := 0; i < 2; i++ {
			got, err := s2.Get(items[i].id)
			if err != nil || !bytes.Equal(got, items[i].d) {
				t.Fatalf("cut %d: earlier record %d lost: %v", cut, i, err)
			}
		}
		if ok, _ := s2.Has(items[2].id); ok {
			t.Fatalf("cut %d: torn record survived", cut)
		}
		if err := s2.Put(items[2].id, items[2].d); err != nil { // writable again after recovery
			t.Fatal(err)
		}
		if got, err := s2.Get(items[2].id); err != nil || !bytes.Equal(got, items[2].d) {
			t.Fatalf("cut %d: rewrite failed: %v", cut, err)
		}
		s2.Close()
	}
}

func TestBitRot(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir)
	items := []item{mk(100), mk(100), mk(100)}
	for _, it := range items {
		s.Put(it.id, it.d)
	}
	s.Sync()

	path := filepath.Join(dir, "packs", "00000001.pack")
	flip := int64(headerSize) + 148 + 50 // inside record 2's payload
	f, _ := os.OpenFile(path, os.O_RDWR, 0)
	b := make([]byte, 1)
	f.ReadAt(b, flip)
	b[0] ^= 0xFF
	f.WriteAt(b, flip)
	f.Close()

	if _, err := s.Get(items[1].id); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("expected ErrCorrupt, got %v", err)
	}
	s.Close()

	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := s2.Has(items[0].id); !ok {
		t.Fatal("record 0 lost")
	}
	if ok, _ := s2.Has(items[2].id); !ok {
		t.Fatal("record 2 lost")
	}
	if ok, _ := s2.Has(items[1].id); ok {
		t.Fatal("corrupt record must not be indexed")
	}
	if s2.Skipped() != 1 {
		t.Fatalf("skipped = %d", s2.Skipped())
	}
}

package pack

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RishiKendai/chunkvault/internals/types"
)

const (
	fileMagic  = "CVPK"
	recMagic   = "CVRC"
	headerSize = 16
	fixedBody  = 36 // chunkID (32) + generation (4)
	defaultMax = 64 << 20
)

var (
	castagnoli  = crc32.MakeTable(crc32.Castagnoli)
	ErrNotFound = errors.New("chunk not found")
	ErrCorrupt  = errors.New("corrupt chunk record")
)

type loc struct {
	pack uint32
	off  int64  // start of the record
	n    uint32 // value of the record's len field
}

type Store struct {
	dir     string
	maxPack int64

	mu         sync.RWMutex
	index      map[types.ChunkID]loc
	files      map[uint32]*os.File
	active     *os.File
	activeSeq  uint32
	activeSize int64
	skipped    int
}

func Open(repo string) (*Store, error) { return OpenSize(repo, defaultMax) }

func OpenSize(repo string, maxPack int64) (*Store, error) {
	dir := filepath.Join(repo, "packs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, maxPack: maxPack, index: map[types.ChunkID]loc{}, files: map[uint32]*os.File{}}

	names, err := filepath.Glob(filepath.Join(dir, "*.pack"))
	if err != nil {
		return nil, err
	}
	sort.Strings(names) // fixed-width hex names sort in numeric order
	for i, name := range names {
		base := strings.TrimSuffix(filepath.Base(name), ".pack")
		v, err := strconv.ParseUint(base, 16, 32)
		if err != nil {
			s.closeAll()
			return nil, fmt.Errorf("bad pack name %q", name)
		}
		seq := uint32(v)
		f, err := os.OpenFile(name, os.O_RDWR, 0)
		if err != nil {
			s.closeAll()
			return nil, err
		}
		s.files[seq] = f
		last := i == len(names)-1
		size, err := s.scan(seq, f, last)
		if err != nil {
			s.closeAll()
			return nil, err
		}
		if last {
			s.active, s.activeSeq, s.activeSize = f, seq, size
		}
	}
	if s.active == nil {
		if err := s.newPack(1); err != nil {
			s.closeAll()
			return nil, err
		}
	}
	return s, nil
}

func (s *Store) closeAll() {
	for _, f := range s.files {
		f.Close()
	}
}

func writeHeader(f *os.File) error {
	h := make([]byte, headerSize)
	copy(h, fileMagic)
	binary.LittleEndian.PutUint16(h[4:], 1) // version
	binary.LittleEndian.PutUint64(h[8:], uint64(time.Now().UnixNano()))
	if _, err := f.WriteAt(h, 0); err != nil {
		return err
	}
	return f.Sync()
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func (s *Store) newPack(seq uint32) error {
	name := filepath.Join(s.dir, fmt.Sprintf("%08x.pack", seq))
	f, err := os.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if err := writeHeader(f); err != nil {
		f.Close()
		return err
	}
	if err := syncDir(s.dir); err != nil { // without this the new file itself may not survive power loss
		f.Close()
		return err
	}
	s.files[seq], s.active, s.activeSeq, s.activeSize = f, f, seq, headerSize
	return nil
}

// scan rebuilds the index from one pack. On the LAST pack, a torn tail is truncated.
// It returns the valid size of the file.
func (s *Store) scan(seq uint32, f *os.File, last bool) (int64, error) {
	st, err := f.Stat()
	if err != nil {
		return 0, err
	}
	size := st.Size()
	if size < headerSize {
		if !last {
			return 0, fmt.Errorf("pack %08x: truncated header", seq)
		}
		if err := f.Truncate(0); err != nil {
			return 0, err
		}
		return headerSize, writeHeader(f)
	}
	hdr := make([]byte, headerSize)
	if _, err := f.ReadAt(hdr, 0); err != nil {
		return 0, err
	}
	if string(hdr[:4]) != fileMagic {
		return 0, fmt.Errorf("pack %08x: bad magic", seq)
	}

	off := int64(headerSize)
	for off+8 <= size {
		var head [8]byte
		if _, err := f.ReadAt(head[:], off); err != nil {
			break
		}
		if string(head[:4]) != recMagic {
			break
		}
		n := int64(binary.LittleEndian.Uint32(head[4:]))
		if n < fixedBody || off+8+n+4 > size {
			break // length garbage or record cut short
		}
		body := make([]byte, n+4)
		if _, err := f.ReadAt(body, off+8); err != nil {
			break
		}
		end := off + 8 + n + 4
		if crc32.Checksum(body[:n], castagnoli) != binary.LittleEndian.Uint32(body[n:]) {
			if last && end == size {
				break // torn final record
			}
			s.skipped++ // bit rot mid-file: skip it, keep going
			off = end
			continue
		}
		var id types.ChunkID
		copy(id[:], body[:32])
		s.index[id] = loc{pack: seq, off: off, n: uint32(n)}
		off = end
	}
	if off != size && last {
		if err := f.Truncate(off); err != nil { // drop the torn tail
			return 0, err
		}
		if err := f.Sync(); err != nil {
			return 0, err
		}
		return off, nil
	}
	if off != size {
		s.skipped++ // unreadable remainder of a sealed pack; verify will flag the missing chunks
	}
	return size, nil
}

func encodeRecord(id types.ChunkID, gen uint32, data []byte) []byte {
	n := fixedBody + len(data)
	rec := make([]byte, 8+n+4)
	copy(rec, recMagic)
	binary.LittleEndian.PutUint32(rec[4:], uint32(n))
	copy(rec[8:], id[:])
	binary.LittleEndian.PutUint32(rec[40:], gen)
	copy(rec[44:], data)
	binary.LittleEndian.PutUint32(rec[8+n:], crc32.Checksum(rec[8:8+n], castagnoli)) // covers id + gen + payload
	return rec
}

func (s *Store) Has(id types.ChunkID) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.index[id]
	return ok, nil
}

// Put appends a record. It does NOT fsync; call Sync before anything depends on durability.
func (s *Store) Put(id types.ChunkID, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.index[id]; ok {
		return nil
	}
	rec := encodeRecord(id, 1, data)
	if s.activeSize+int64(len(rec)) > s.maxPack && s.activeSize > headerSize {
		if err := s.active.Sync(); err != nil { // seal the old pack durably, then start a new one
			return err
		}
		if err := s.newPack(s.activeSeq + 1); err != nil {
			return err
		}
	}
	if _, err := s.active.WriteAt(rec, s.activeSize); err != nil {
		_ = s.active.Truncate(s.activeSize)
		return err
	}
	s.index[id] = loc{pack: s.activeSeq, off: s.activeSize, n: uint32(fixedBody + len(data))}
	s.activeSize += int64(len(rec))
	return nil
}

func (s *Store) Get(id types.ChunkID) ([]byte, error) {
	s.mu.RLock()
	l, ok := s.index[id]
	f := s.files[l.pack]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	n := int(l.n)
	rec := make([]byte, 8+n+4)
	if _, err := f.ReadAt(rec, l.off); err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrCorrupt, id, err)
	}
	if string(rec[:4]) != recMagic ||
		crc32.Checksum(rec[8:8+n], castagnoli) != binary.LittleEndian.Uint32(rec[8+n:]) ||
		!bytes.Equal(rec[8:40], id[:]) {
		return nil, fmt.Errorf("%w: %s", ErrCorrupt, id)
	}
	return rec[44 : 8+n], nil
}

func (s *Store) Sync() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active.Sync()
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.active.Sync()
	s.closeAll()
	return err
}

func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.index)
}

// Skipped is the number of corrupt or unreadable records found at open.
func (s *Store) Skipped() int { return s.skipped }

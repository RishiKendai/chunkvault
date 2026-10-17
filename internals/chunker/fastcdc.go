package chunker

import (
	"fmt"
	"io"
	"math/bits"
)

type Params struct {
	Min, Avg, Max int
	NormLevel     int
	Seed          uint64
}

var DefaultParams = Params{
	Min:       512 << 10,
	Avg:       1 << 20,
	Max:       4 << 20,
	NormLevel: 2,
	Seed:      0xC0FFEE,
}

type FastCDC struct {
	src          io.Reader
	p            Params
	gear         [256]uint64
	maskS, maskL uint64
	buf          []byte
	pos, end     int
	eof          bool
	off          int64
}

func NewFastCDC(r io.Reader, p Params) (*FastCDC, error) {
	if p.Min <= 0 || p.Min >= p.Avg || p.Avg >= p.Max || p.Avg&(p.Avg-1) != 0 {
		return nil, fmt.Errorf("invalid params: need 0 < Min < Avg < Max and Avg a power of two")
	}
	b := bits.Len(uint(p.Avg)) - 1 // log2(Avg)
	f := &FastCDC{
		src:   r,
		p:     p,
		gear:  gearTable(p.Seed),
		maskS: spreadMask(b + p.NormLevel),
		maskL: spreadMask(b - p.NormLevel),
		buf:   make([]byte, 2*p.Max),
	}
	return f, nil
}

func splitmix64(x *uint64) uint64 {
	*x += 0x9e3779b97f4a7c15
	z := *x
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

func gearTable(seed uint64) [256]uint64 {
	var t [256]uint64
	s := seed
	for i := range t {
		t[i] = splitmix64(&s)
	}
	return t
}

// spreadMask sets n bits spread across the 64-bit word (not just the low bits).
func spreadMask(n int) uint64 {
	var m uint64
	for i := 0; i < n; i++ {
		m |= 1 << uint(i*64/n)
	}
	return m
}

func (f *FastCDC) fill() error {
	copy(f.buf, f.buf[f.pos:f.end])
	f.end -= f.pos
	f.pos = 0
	n, err := io.ReadFull(f.src, f.buf[f.end:])
	f.end += n
	switch err {
	case nil:
	case io.EOF, io.ErrUnexpectedEOF:
		f.eof = true
	default:
		return err
	}
	return nil
}

func (f *FastCDC) cut(data []byte) int {
	n := len(data)
	if n <= f.p.Min {
		return n
	}
	if n > f.p.Max {
		n = f.p.Max
	}
	normal := f.p.Avg
	if normal > n {
		normal = n
	}
	var fp uint64
	i := f.p.Min // no cut before Min
	for ; i < normal; i++ {
		fp = (fp << 1) + f.gear[data[i]]
		if fp&f.maskS == 0 {
			return i + 1
		}
	}
	for ; i < n; i++ {
		fp = (fp << 1) + f.gear[data[i]]
		if fp&f.maskL == 0 {
			return i + 1
		}
	}
	return n // forced cut at Max, or end of data
}

func (f *FastCDC) Next() (Chunk, error) {
	if f.end-f.pos < f.p.Max && !f.eof {
		if err := f.fill(); err != nil {
			return Chunk{}, err
		}
	}
	if f.pos == f.end {
		return Chunk{}, io.EOF
	}
	avail := f.buf[f.pos:f.end]
	n := f.cut(avail)
	data := make([]byte, n) // copied out; buffer pooling comes with the pipeline
	copy(data, avail[:n])
	c := Chunk{Data: data, Offset: f.off}
	f.pos += n
	f.off += int64(n)
	return c, nil
}

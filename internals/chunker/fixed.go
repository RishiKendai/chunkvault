package chunker

import "io"

type Chunk struct {
	Data   []byte
	Offset int64
}

// Fixed size chunker implementation
type Chunker interface {
	Next() (Chunk, error)
}

type Fixed struct {
	r    io.Reader
	size int
	off  int64
}

func NewFixed(r io.Reader, size int) *Fixed { return &Fixed{r: r, size: size} }

func (f *Fixed) Next() (Chunk, error) {
	buf := make([]byte, f.size)
	n, err := io.ReadFull(f.r, buf)
	switch err {
	case nil, io.ErrUnexpectedEOF:
	case io.EOF:
		return Chunk{}, io.EOF
	default:
		return Chunk{}, err
	}
	c := Chunk{Data: buf[:n], Offset: f.off}
	f.off += int64(n)
	return c, nil
}

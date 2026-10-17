package chunker

import (
	"bytes"
	"crypto/rand"
	"io"
	"testing"
)

func TestFixed(t *testing.T) {
	const sz = 1 << 20
	cases := []struct {
		name   string
		n      int
		chunks int
	}{
		{"empty", 0, 0},
		{"small", 10, 1},
		{"exact", sz, 1},
		{"exact+1", sz + 1, 2},
		{"three", 3 * sz, 3},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := make([]byte, tc.n)
			rand.Read(in)
			ck := NewFixed(bytes.NewReader(in), sz)
			var out []byte
			count := 0
			for {
				c, err := ck.Next()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				count++
				out = append(out, c.Data...)
			}
			if count != tc.chunks {
				t.Fatalf("chunks = %d, want %d", count, tc.chunks)
			}
			if !bytes.Equal(in, out) {
				t.Fatal("reassembled data differs")
			}
		})
	}
}

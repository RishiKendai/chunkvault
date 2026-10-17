package chunker

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"io"
	"testing"
)

func collect(t *testing.T, data []byte) [][]byte {
	t.Helper()
	ck, err := NewFastCDC(bytes.NewReader(data), DefaultParams)
	if err != nil {
		t.Fatal(err)
	}
	var out [][]byte
	for {
		c, err := ck.Next()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, c.Data)
	}
}

func TestFastCDCReassemblyAndBounds(t *testing.T) {
	for _, n := range []int{0, 1, 100, 512 << 10, 5 << 20, 30 << 20} {
		in := make([]byte, n)
		rand.Read(in)
		chunks := collect(t, in)
		var joined []byte
		for i, c := range chunks {
			joined = append(joined, c...)
			last := i == len(chunks)-1
			if len(c) > DefaultParams.Max || (!last && len(c) < DefaultParams.Min) {
				t.Fatalf("n=%d chunk %d has bad size %d", n, i, len(c))
			}
		}
		if !bytes.Equal(in, joined) {
			t.Fatalf("n=%d: reassembly differs", n)
		}
	}
}

func TestFastCDCDeterministic(t *testing.T) {
	in := make([]byte, 20<<20)
	rand.Read(in)
	a, b := collect(t, in), collect(t, in)
	if len(a) != len(b) {
		t.Fatal("different chunk counts")
	}
	for i := range a {
		if !bytes.Equal(a[i], b[i]) {
			t.Fatalf("chunk %d differs", i)
		}
	}
}

func TestFastCDCInsertOneByte(t *testing.T) {
	in := make([]byte, 64<<20)
	rand.Read(in)
	shifted := append([]byte{0x42}, in...)
	set := map[[32]byte]bool{}
	a := collect(t, in)
	for _, c := range a {
		set[sha256.Sum256(c)] = true
	}
	shared := 0
	for _, c := range collect(t, shifted) {
		if set[sha256.Sum256(c)] {
			shared++
		}
	}
	if float64(shared)/float64(len(a)) < 0.9 {
		t.Fatalf("only %d of %d chunks shared", shared, len(a))
	}
}

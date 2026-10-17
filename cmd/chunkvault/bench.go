package main

import (
	"bytes"
	crand "crypto/rand"
	"fmt"
	"io"
	"time"

	"github.com/RishiKendai/chunkvault/internals/chunker"
	"github.com/RishiKendai/chunkvault/internals/crypto"
	"github.com/RishiKendai/chunkvault/internals/types"
)

func idSet(ck chunker.Chunker) (map[types.ChunkID]struct{}, error) {
	ider := crypto.SHA256IDer{}
	set := map[types.ChunkID]struct{}{}

	for {
		c, err := ck.Next()
		if err == io.EOF {
			return set, nil
		}
		if err != nil {
			return nil, err
		}
		set[ider.ID(c.Data)] = struct{}{}
	}
}

func bench(args []string) error {
	const size = 100 << 20
	data := make([]byte, size)
	if _, err := crand.Read(data); err != nil {
		return err
	}
	shifted := append([]byte{0x42}, data...)

	cases := []struct {
		name string
		mk   func(io.Reader) (chunker.Chunker, error)
	}{
		{"fized-1MiB", func(r io.Reader) (chunker.Chunker, error) { return chunker.NewFixed(r, chunkSize), nil }},
		{"fastcdc", func(r io.Reader) (chunker.Chunker, error) { return chunker.NewFastCDC(r, chunker.DefaultParams) }},
	}
	fmt.Printf("%-12s %8s %8s %8s %10s\n", "chunker", "chunks", "shared", "shared%", "MB/s")
	for _, tc := range cases {
		ck, err := tc.mk(bytes.NewReader(data))
		if err != nil {
			return err
		}
		start := time.Now()
		a, err := idSet(ck)
		if err != nil {
			return err
		}
		mbps := float64(size>>20) / time.Since(start).Seconds()

		ck2, err := tc.mk(bytes.NewReader(shifted))
		if err != nil {
			return err
		}
		b, err := idSet(ck2)
		if err != nil {
			return err
		}
		shared := 0
		for id := range a {
			if _, ok := b[id]; ok {
				shared++
			}
		}
		fmt.Printf("%-12s %8d %8d %7.1f%% %10.0f\n", tc.name, len(a), shared, 100*float64(shared)/float64(len(a)), mbps)
	}
	return nil
}

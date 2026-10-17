package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/RishiKendai/chunkvault/internals/chunker"
	"github.com/RishiKendai/chunkvault/internals/crypto"
	"github.com/RishiKendai/chunkvault/internals/store"
	"github.com/RishiKendai/chunkvault/internals/types"
)

const chunkSize = 1 << 20

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: chunkvault backup | restore ...")
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "backup":
		err = backup(os.Args[2:])
	case "restore":
		err = restore(os.Args[2:])
	case "bench":
		err = bench(os.Args[2:])
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func backup(args []string) error {
	fs := flag.NewFlagSet("backup", flag.ExitOnError)
	repo := fs.String("repo", "./cv-repo", "repository directory")
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: backup [--repo dir] <file>")
	}
	src := fs.Arg(0)

	st, err := store.NewLocal(*repo)
	if err != nil {
		return err
	}
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()

	// var ck chunker.Chunker = chunker.NewFixed(f, chunkSize)
	ck, err := chunker.NewFastCDC(f, chunker.DefaultParams)
	ider := crypto.SHA256IDer{}

	var ids []types.ChunkID
	newChunks := 0

	for {
		c, err := ck.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		id := ider.ID(c.Data)
		has, err := st.Has(id)
		if err != nil {
			return err
		}

		if !has {
			if err := st.Put(id, c.Data); err != nil {
				return err
			}
			newChunks++
		}
		ids = append(ids, id)
	}
	listDir := filepath.Join(*repo, "lists")
	if err := os.MkdirAll(listDir, 0o755); err != nil {
		return err
	}
	listPath := filepath.Join(listDir, filepath.Base(src)+".list")
	var sb strings.Builder
	for _, id := range ids {
		sb.WriteString(id.String() + "\n")
	}
	if err := os.WriteFile(listPath, []byte(sb.String()), 0o644); err != nil {
		return err
	}
	fmt.Printf("chunks: %d total, %d new, %d deduped\nlist: %s\n", len(ids), newChunks, len(ids)-newChunks, listPath)
	return nil
}

func restore(args []string) error {
	fs := flag.NewFlagSet("restore", flag.ExitOnError)
	repo := fs.String("repo", "./cv-repo", "repository directory")
	fs.Parse(args)
	if fs.NArg() != 2 {
		return fmt.Errorf("usage: restore [--repo dir] <list-file> <out-file>")
	}

	st, err := store.NewLocal(*repo)
	if err != nil {
		return err
	}
	lf, err := os.Open(fs.Arg(0))
	if err != nil {
		return err
	}
	defer lf.Close()
	out, err := os.Create(fs.Arg(1))
	if err != nil {
		return err
	}
	defer out.Close()

	ider := crypto.SHA256IDer{}
	sc := bufio.NewScanner(lf)
	for sc.Scan() {
		id, err := types.ParseChunkID(strings.TrimSpace(sc.Text()))
		if err != nil {
			return err
		}
		data, err := st.Get(id)
		if err != nil {
			return fmt.Errorf("chunk %s: %w", id, err)
		}
		if ider.ID(data) != id {
			return fmt.Errorf("chunk %s is corrupt", id)
		}
		if _, err := out.Write(data); err != nil {
			return err
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return out.Close()
}

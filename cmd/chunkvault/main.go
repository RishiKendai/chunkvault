package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/RishiKendai/chunkvault/internals/archiver"
	"github.com/RishiKendai/chunkvault/internals/chunker"
	"github.com/RishiKendai/chunkvault/internals/crypto"
	"github.com/RishiKendai/chunkvault/internals/snapshot"

	// "github.com/RishiKendai/chunkvault/internals/store"
	"github.com/RishiKendai/chunkvault/internals/pack"
)

const chunkSize = 1 << 20 // used by the fixed chunker in bench.go

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: chunkvault backup|snapshots|restore|verify|bench ...")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "backup":
		err = backup(os.Args[2:])
	case "snapshots":
		err = snapshots(os.Args[2:])
	case "restore":
		err = restore(os.Args[2:])
	case "verify":
		err = verify(os.Args[2:])
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

func openRepo(repo string) (*archiver.Archiver, *snapshot.Catalog, error) {
	absRepo, err := filepath.Abs(repo)
	if err != nil {
		return nil, nil, err
	}
	// st, err := store.NewLocal(absRepo)
	st, err := pack.Open(absRepo)
	if err != nil {
		return nil, nil, err
	}
	cat, err := snapshot.NewCatalog(absRepo)
	if err != nil {
		return nil, nil, err
	}
	a := &archiver.Archiver{Store: st, IDer: crypto.SHA256IDer{}, Params: chunker.DefaultParams, Exclude: absRepo}
	return a, cat, nil
}

func backup(args []string) error {
	fs := flag.NewFlagSet("backup", flag.ExitOnError)
	repo := fs.String("repo", "./cv-repo", "repository directory")
	fs.Parse(args) // flags must come BEFORE the path
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: backup [--repo dir] <file-or-dir>")
	}
	abs, err := filepath.Abs(fs.Arg(0))
	if err != nil {
		return err
	}
	a, cat, err := openRepo(*repo)
	if err != nil {
		return err
	}
	parent, err := cat.LatestForPath(abs)
	if err != nil {
		return err
	}
	snap, err := a.Run(abs, parent)
	if err != nil {
		return err
	}
	if err := cat.Commit(snap); err != nil {
		return err
	}
	s := snap.Stats
	fmt.Printf("snapshot %s\n", snap.ID)
	fmt.Printf("files: %d (%d unchanged, skipped)\n", s.Files, s.FilesReused)
	fmt.Printf("read: %.1f MB | new chunks: %d (%.1f MB) | deduped chunks: %d\n",
		float64(s.BytesRead)/1e6, s.NewChunks, float64(s.NewBytes)/1e6, s.DedupChunks)
	return nil
}

func snapshots(args []string) error {
	fs := flag.NewFlagSet("snapshots", flag.ExitOnError)
	repo := fs.String("repo", "./cv-repo", "repository directory")
	fs.Parse(args)
	_, cat, err := openRepo(*repo)
	if err != nil {
		return err
	}
	all, err := cat.List()
	if err != nil {
		return err
	}
	fmt.Printf("%-26s %-19s %6s %9s %10s  %s\n", "ID", "TIME", "FILES", "NEWCHUNKS", "NEWBYTES", "PATH")
	for _, s := range all {
		fmt.Printf("%-26s %-19s %6d %9d %9.1fM  %s\n", s.ID,
			time.Unix(0, s.CommittedAt).Local().Format("2006-01-02 15:04:05"),
			s.Stats.Files, s.Stats.NewChunks, float64(s.Stats.NewBytes)/1e6, s.Paths[0])
	}
	return nil
}

func restore(args []string) error {
	fs := flag.NewFlagSet("restore", flag.ExitOnError)
	repo := fs.String("repo", "./cv-repo", "repository directory")
	fs.Parse(args)
	if fs.NArg() != 2 {
		return fmt.Errorf("usage: restore [--repo dir] <snapshot-id|latest> <target-dir>")
	}
	a, cat, err := openRepo(*repo)
	if err != nil {
		return err
	}
	snap, err := cat.Get(fs.Arg(0))
	if err != nil {
		return err
	}
	if err := a.Restore(snap.RootTree, fs.Arg(1)); err != nil {
		return err
	}
	fmt.Printf("restored snapshot %s to %s\n", snap.ID, fs.Arg(1))
	return nil
}

func verify(args []string) error {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	repo := fs.String("repo", "./cv-repo", "repository directory")
	readData := fs.Bool("read-data", false, "read and re-hash every chunk")
	fs.Parse(args)
	a, cat, err := openRepo(*repo)
	if err != nil {
		return err
	}
	all, err := cat.List()
	if err != nil {
		return err
	}
	bad := 0
	for _, s := range all {
		problems, err := a.Verify(s.RootTree, *readData)
		if err != nil {
			return err
		}
		if len(problems) == 0 {
			fmt.Printf("OK    %s\n", s.ID)
			continue
		}
		bad++
		fmt.Printf("BAD   %s\n", s.ID)
		for _, p := range problems {
			fmt.Printf("      %s\n", p)
		}
	}
	if bad > 0 {
		return fmt.Errorf("%d snapshot(s) have problems", bad)
	}
	return nil
}

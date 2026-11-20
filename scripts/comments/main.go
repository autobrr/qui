// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/autobrr/qui/scripts/comments/gocomments"
)

const usage = `usage:
  go run ./scripts/comments list             print each comment block of the tracked Go files as a JSON line
  go run ./scripts/comments strip <file>     delete the Go comment blocks that the JSON lines in <file> name`

func main() {
	var err error
	switch {
	case len(os.Args) == 2 && os.Args[1] == "list":
		err = list()
	case len(os.Args) == 3 && os.Args[1] == "strip":
		err = strip(os.Args[2])
	default:
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "comments:", err)
		os.Exit(1)
	}
}

func list() error {
	paths, err := gocomments.Tracked()
	if err != nil {
		return err
	}
	for _, path := range paths {
		src, err := os.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		blocks, err := gocomments.Blocks(path, src)
		if err != nil {
			return err
		}
		for _, block := range blocks {
			if err := json.MarshalWrite(os.Stdout, block); err != nil {
				return err
			}
			fmt.Println()
		}
	}
	return nil
}

func strip(listPath string) error {
	//nolint:gosec // the caller names the list
	raw, err := os.ReadFile(listPath)
	if err != nil {
		return err
	}
	doomed := map[string][]gocomments.Block{}
	for line := range strings.SplitSeq(string(raw), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var block gocomments.Block
		if err := json.Unmarshal([]byte(line), &block); err != nil {
			return fmt.Errorf("%s: %w", listPath, err)
		}
		if strings.HasSuffix(block.File, ".go") {
			doomed[block.File] = append(doomed[block.File], block)
		}
	}
	deleted, lost := 0, 0
	for path, blocks := range doomed {
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out, missed, err := gocomments.Strip(path, src, blocks)
		if err != nil {
			return err
		}
		for _, block := range missed {
			fmt.Printf("%s:%d: no comment block with this text starts here, skipped\n", block.File, block.Line)
		}
		deleted += len(blocks) - len(missed)
		lost += len(missed)
		if err := replaceFile(path, out); err != nil {
			return err
		}
	}
	fmt.Printf("comments: deleted %d blocks, skipped %d\n", deleted, lost)
	return nil
}

func replaceFile(path string, data []byte) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	_, err = tmp.Write(data)
	err = errors.Join(err, tmp.Chmod(info.Mode().Perm()), tmp.Sync(), tmp.Close())
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return os.Rename(tmp.Name(), path)
}

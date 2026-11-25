// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"slices"
	"strings"

	"github.com/autobrr/qui/scripts/comments/gocomments"
)

const touchedCommentAdvice = `comments: delete each comment above. If it holds a fact that the code must keep, first put that fact in a name, a type, a test, or an ADR. See "Comments" in docs/standards/simplicity.md.`

type attachedComment struct {
	text, code string
}

func attached(b gocomments.Block) attachedComment {
	return attachedComment{text: strings.TrimRight(strings.Join(strings.Fields(b.Text), " "), " /"), code: b.Code}
}

func touchedCommentProblems(base string) ([]string, error) {
	out, err := exec.CommandContext(context.Background(), "git", "merge-base", base, "HEAD").Output()
	if err != nil {
		return nil, fmt.Errorf("git merge-base %s HEAD: %w", base, err)
	}
	fork := string(bytes.TrimSpace(out))
	files, err := changedGoFiles(fork)
	if err != nil {
		return nil, err
	}
	blobs, err := baseBlobs(fork, files)
	if err != nil {
		return nil, err
	}
	now, was := map[string][]gocomments.Block{}, map[string][]gocomments.Block{}
	for _, file := range files {
		if was[file], err = blocksOf(file, blobs[file]); err != nil {
			return nil, fmt.Errorf("%s at %s: %w", file, fork, err)
		}
		src, err := os.ReadFile(file)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		if now[file], err = blocksOf(file, src); err != nil {
			return nil, err
		}
	}
	var problems []string
	for _, b := range touchedComments(now, was) {
		problems = append(problems, fmt.Sprintf("%s:%d: this comment is new or changed, or it is on code that changed.", b.File, b.Line))
	}
	if len(problems) > 0 {
		problems = append(problems, touchedCommentAdvice)
	}
	return problems, nil
}

func changedGoFiles(fork string) ([]string, error) {
	out, err := exec.CommandContext(context.Background(), "git", "diff", "--name-only", "--no-renames", "-z", fork, "--", "*.go").Output()
	if err != nil {
		return nil, fmt.Errorf("git diff %s: %w", fork, err)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00"), nil
}

func blocksOf(file string, src []byte) ([]gocomments.Block, error) {
	if len(src) == 0 {
		return nil, nil
	}
	return gocomments.Blocks(file, src)
}

func touchedComments(now, was map[string][]gocomments.Block) []gocomments.Block {
	held := map[string]map[attachedComment]int{}
	for file, blocks := range was {
		held[file] = map[attachedComment]int{}
		for _, b := range blocks {
			held[file][attached(b)]++
		}
	}
	var strangers []gocomments.Block
	for _, file := range slices.Sorted(maps.Keys(now)) {
		for _, b := range now[file] {
			if held[file][attached(b)] == 0 {
				strangers = append(strangers, b)
				continue
			}
			held[file][attached(b)]--
		}
	}
	gone := map[attachedComment]int{}
	for _, comments := range held {
		for c, n := range comments {
			gone[c] += n
		}
	}
	return slices.DeleteFunc(strangers, func(b gocomments.Block) bool {
		moved := gone[attached(b)] > 0
		if moved {
			gone[attached(b)]--
		}
		return moved
	})
}

// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path"
	"slices"
	"strconv"
	"strings"
)

const (
	sizeLimitPath = ".file-size.json"
	bytesPerToken = 4

	sizeAdvice = `file size: an agent reads a whole file, so a large file fills its context. Move code out of each file above, in a commit of its own that only moves code:
  1. Move one whole responsibility: the functions and types that are used together, not only the lines that you added.
  2. Move it to a file in the same folder whose name fits and that stays under the limit. If there is none, make a new file in the same folder.
  3. Name the file for what it holds, in the words of GLOSSARY.md.
  4. Move the tests with the code that they test.
  5. If the move needs a new folder or a new exported name, stop and ask the maintainer.
A generated file needs an entry in "skip" of ` + sizeLimitPath + `, which only a maintainer adds: ask. See "File Size" in docs/linting.md.`

	looserAdvice = sizeLimitPath + `: only a maintainer makes the limit looser, by hand. Take the change back, and move code out of the file that is too large. If the file is generated or the move needs design, stop and ask the maintainer. See "File Size" in docs/linting.md.`
)

type sizeLimit struct {
	MaxBytes   int      `json:"max_bytes"`
	Extensions []string `json:"extensions"`
	Skip       []string `json:"skip"`
}

func parseSizeLimit(raw []byte) (sizeLimit, error) {
	var limit sizeLimit
	if err := json.Unmarshal(raw, &limit, json.RejectUnknownMembers(true)); err != nil {
		return limit, err
	}
	if limit.MaxBytes <= 0 {
		return limit, errors.New("max_bytes must be above 0")
	}
	return limit, nil
}

func (l sizeLimit) covers(file string) bool {
	if !slices.Contains(l.Extensions, path.Ext(file)) {
		return false
	}
	return !slices.ContainsFunc(l.Skip, func(skip string) bool {
		return file == skip || (strings.HasSuffix(skip, "/") && strings.HasPrefix(file, skip))
	})
}

func fileSizeProblems(base string) ([]string, error) {
	raw, err := os.ReadFile(sizeLimitPath)
	if err != nil {
		return nil, err
	}
	limit, err := parseSizeLimit(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", sizeLimitPath, err)
	}
	var problems []string
	//nolint:gosec // the developer or CI picks -base
	if baseRaw, err := exec.CommandContext(context.Background(), "git", "show", base+":"+sizeLimitPath).Output(); err == nil {
		was, err := parseSizeLimit(baseRaw)
		if err != nil {
			return nil, fmt.Errorf("%s at %s: %w", sizeLimitPath, base, err)
		}
		problems = looserLimit(limit, was)
	} else {
		fmt.Printf("ratchet: no %s at %s, skipping its growth check\n", sizeLimitPath, base)
	}
	now, err := coveredSizes(limit)
	if err != nil {
		return nil, err
	}
	out, err := exec.CommandContext(context.Background(), "git", "diff", "--name-status", "-M", "-z", "--diff-filter=R", base, "--").Output()
	if err != nil {
		return nil, fmt.Errorf("git diff %s: %w", base, err)
	}
	renamed := parseRenames(out)
	var oversized []string
	for file, size := range now {
		if size > limit.MaxBytes {
			oversized = append(oversized, cmp.Or(renamed[file], file))
		}
	}
	was, err := baseSizes(base, oversized)
	if err != nil {
		return nil, err
	}
	return append(problems, sizeProblems(limit, now, was, renamed)...), nil
}

func baseSizes(base string, files []string) (map[string]int, error) {
	var objects strings.Builder
	for _, file := range files {
		objects.WriteString(base + ":" + file + "\x00")
	}
	cmd := exec.CommandContext(context.Background(), "git", "cat-file", "--batch", "-z")
	cmd.Stdin = strings.NewReader(objects.String())
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git cat-file %s: %w", base, err)
	}
	return parseBlobSizes(files, out)
}

func coveredSizes(limit sizeLimit) (map[string]int, error) {
	out, err := exec.CommandContext(context.Background(), "git", "ls-files", "-z", "--cached", "--others", "--exclude-standard").Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %w", err)
	}
	sizes := map[string]int{}
	for file := range strings.SplitSeq(strings.TrimSuffix(string(out), "\x00"), "\x00") {
		if !limit.covers(file) {
			continue
		}
		src, err := os.ReadFile(file)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		sizes[file] = sizeOf(src)
	}
	return sizes, nil
}

func sizeOf(src []byte) int {
	return len(src) - bytes.Count(src, []byte("\r\n"))
}

func parseBlobSizes(files []string, out []byte) (map[string]int, error) {
	sizes := map[string]int{}
	for _, file := range files {
		header, rest, ok := bytes.Cut(out, []byte("\n"))
		if !ok {
			return nil, fmt.Errorf("git cat-file: no header for %s", file)
		}
		if bytes.HasSuffix(header, []byte(" missing")) {
			out = rest
			continue
		}
		fields := strings.Fields(string(header))
		if len(fields) != 3 {
			return nil, fmt.Errorf("git cat-file: bad header %q for %s", header, file)
		}
		size, err := strconv.Atoi(fields[2])
		if err != nil || len(rest) <= size {
			return nil, fmt.Errorf("git cat-file: bad header %q for %s", header, file)
		}
		if fields[1] == "blob" {
			sizes[file] = sizeOf(rest[:size])
		}
		out = rest[size+1:]
	}
	return sizes, nil
}

func parseRenames(out []byte) map[string]string {
	fields := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	renamed := map[string]string{}
	for entry := range slices.Chunk(fields, 3) {
		if len(entry) == 3 {
			renamed[entry[2]] = entry[1]
		}
	}
	return renamed
}

func looserLimit(now, was sizeLimit) []string {
	var problems []string
	if now.MaxBytes > was.MaxBytes {
		problems = append(problems, fmt.Sprintf("%s: max_bytes grew from %d to %d.", sizeLimitPath, was.MaxBytes, now.MaxBytes))
	}
	for _, skip := range now.Skip {
		if !slices.Contains(was.Skip, skip) {
			problems = append(problems, fmt.Sprintf("%s: skip has the new entry %s.", sizeLimitPath, skip))
		}
	}
	for _, extension := range was.Extensions {
		if !slices.Contains(now.Extensions, extension) {
			problems = append(problems, fmt.Sprintf("%s: extensions lost the entry %s.", sizeLimitPath, extension))
		}
	}
	if len(problems) > 0 {
		problems = append(problems, looserAdvice)
	}
	return problems
}

func sizeProblems(limit sizeLimit, now, was map[string]int, renamed map[string]string) []string {
	var problems []string
	for _, file := range slices.Sorted(maps.Keys(now)) {
		size := now[file]
		if size <= limit.MaxBytes {
			continue
		}
		old := file
		if from, ok := renamed[file]; ok {
			old = from
		}
		before, existed := was[old]
		switch {
		case !existed:
			problems = append(problems, fmt.Sprintf("%s: a new file of %d bytes. The limit in %s is %d bytes, about %d tokens.", file, size, sizeLimitPath, limit.MaxBytes, limit.MaxBytes/bytesPerToken))
		case before <= limit.MaxBytes:
			problems = append(problems, fmt.Sprintf("%s: grew from %d to %d bytes. The limit in %s is %d bytes, about %d tokens.", file, before, size, sizeLimitPath, limit.MaxBytes, limit.MaxBytes/bytesPerToken))
		}
	}
	if len(problems) > 0 {
		problems = append(problems, sizeAdvice)
	}
	return problems
}

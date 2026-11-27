// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sshtest

import (
	"context"
	"errors"
	"os/exec"
	"strings"

	"golang.org/x/crypto/ssh"
)

func (s *Server) runShell(channel ssh.Channel, request, script string) uint32 {
	s.mu.Lock()
	s.commands = append(s.commands, shellWords(script))
	s.mu.Unlock()

	cmd := exec.CommandContext(context.Background(), "sh", "-c", request)
	cmd.Stdin = strings.NewReader(script)
	cmd.Stdout = channel
	cmd.Stderr = channel.Stderr()
	err := cmd.Run()
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		return uint32(exitErr.ExitCode())
	}
	if err != nil {
		return 127
	}
	return 0
}

func (s *Server) Commands() [][]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][]string(nil), s.commands...)
}

func shellWords(line string) []string {
	var words []string
	var word strings.Builder
	inWord, quoted := false, false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case quoted && c == '\'':
			quoted = false
		case quoted:
			word.WriteByte(c)
		case c == '\'':
			quoted, inWord = true, true
		case c == '\\' && i+1 < len(line):
			i++
			word.WriteByte(line[i])
			inWord = true
		case c == ' ' || c == '\t' || c == '\n':
			if inWord {
				words = append(words, word.String())
				word.Reset()
				inWord = false
			}
		default:
			word.WriteByte(c)
			inWord = true
		}
	}
	if inWord {
		words = append(words, word.String())
	}
	return words
}

func quotedAny(line string, match func(string) bool) bool {
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '\\':
			i++
		case '\'':
			end := strings.IndexByte(line[i+1:], '\'')
			if end < 0 {
				return match(line[i+1:])
			}
			if match(line[i+1 : i+1+end]) {
				return true
			}
			i += 1 + end
		}
	}
	return false
}

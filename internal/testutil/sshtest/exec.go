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

// runShell runs request through the local sh with script on its stdin, the
// way sshd hands a session to the login shell, against the filesystem the
// sftp subsystem serves. It records the words of script for the quoting tests.
func (s *Server) runShell(channel ssh.Channel, request, script string) uint32 {
	s.mu.Lock()
	s.commands = append(s.commands, shellWords(script))
	s.mu.Unlock()

	// The command ends on its own: a client that gives up closes only the
	// channel, which a command that writes nothing does not see.

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

// Commands returns the words of each command the shell read from the stdin
// of a session, in order.
func (s *Server) Commands() [][]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][]string(nil), s.commands...)
}

// shellWords splits a command line the way a POSIX shell splits words made of
// bare text, single-quoted text and backslash escapes. It covers what the
// client sends, not the whole shell grammar.
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

// quotedAny reports whether match holds for the text of any single-quoted part
// of a command line, read the POSIX way, so a mode can model a shell that
// reads that text differently. It knows only single quotes and backslash
// escapes, not double quotes, which shellQuote never writes.
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

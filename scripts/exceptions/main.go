// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"encoding/json/v2"
	"fmt"
	"go/scanner"
	"go/token"
	"os"
)

type source struct {
	Name string `json:"name"`
	Text string `json:"text"`
}

type comment struct {
	Line int    `json:"line"`
	Text string `json:"text"`
}

type listing struct {
	Comments []comment `json:"comments"`
	Broken   bool      `json:"broken"`
}

func list(src source) listing {
	fset := token.NewFileSet()
	file := fset.AddFile(src.Name, fset.Base(), len(src.Text))
	var s scanner.Scanner
	s.Init(file, []byte(src.Text), nil, scanner.ScanComments)
	result := listing{Comments: []comment{}}
	for {
		pos, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}
		if tok == token.COMMENT {
			result.Comments = append(result.Comments, comment{Line: file.PositionFor(pos, false).Line, Text: lit})
		}
	}
	result.Broken = s.ErrorCount > 0
	return result
}

func run() error {
	var sources []source
	if err := json.UnmarshalRead(os.Stdin, &sources); err != nil {
		return err
	}
	listings := make([]listing, 0, len(sources))
	for _, src := range sources {
		listings = append(listings, list(src))
	}
	return json.MarshalWrite(os.Stdout, listings)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "exceptions:", err)
		os.Exit(1)
	}
}

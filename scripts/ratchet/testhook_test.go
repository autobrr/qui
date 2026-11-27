// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCountTestHooks(t *testing.T) {
	const service = `package crossseed

import "context"

type loader func(context.Context) error

type Service struct {
	invoker     func(context.Context) error
	settings    loader
	credentials func() string
	store       *Store
}

func NewService(store *Store) *Service {
	return &Service{store: store, credentials: store.Credentials}
}
`
	tests := []struct {
		name  string
		files map[string]string
		want  map[string]int
	}{
		{
			name: "a nil check on a field that only a test sets",
			files: map[string]string{
				"a/run.go":      `package crossseed; func (s *Service) run(ctx context.Context) error { if s.invoker != nil { return s.invoker(ctx) }; return nil }`,
				"a/run_test.go": `package crossseed; func TestRun(t *testing.T) { s := &Service{invoker: func(context.Context) error { return nil }}; s.invoker = nil }`,
			},
			want: map[string]int{"a/run.go": 1},
		},
		{
			name:  "a field of a named function type",
			files: map[string]string{"a/run.go": `package crossseed; func (s *Service) run(ctx context.Context) error { if s.settings == nil { return nil }; return s.settings(ctx) }`},
			want:  map[string]int{"a/run.go": 1},
		},
		{
			name:  "a copy to a local in the if statement",
			files: map[string]string{"a/run.go": `package crossseed; func (s *Service) run(ctx context.Context) error { if run := s.invoker; run != nil { return run(ctx) }; return nil }`},
			want:  map[string]int{"a/run.go": 1},
		},
		{
			name:  "a copy in a var declaration",
			files: map[string]string{"a/run.go": `package crossseed; func (s *Service) run(ctx context.Context) error { var run = s.invoker; if run == nil { return nil }; return run(ctx) }`},
			want:  map[string]int{"a/run.go": 1},
		},
		{
			name:  "a field that the constructor sets",
			files: map[string]string{"a/run.go": `package crossseed; func (s *Service) run() string { if s.credentials == nil { return "" }; return s.credentials() }`},
			want:  map[string]int{},
		},
		{
			name: "a field that a setter sets",
			files: map[string]string{
				"a/set.go": `package crossseed; func (s *Service) SetInvoker(fn func(context.Context) error) { s.invoker = fn }`,
				"a/run.go": `package crossseed; func (s *Service) run(ctx context.Context) error { if s.invoker == nil { return nil }; return s.invoker(ctx) }`,
			},
			want: map[string]int{},
		},
		{
			name: "a field of the same name that another package sets",
			files: map[string]string{
				"b/b.go":   `package other; type Options struct { invoker func() }; func New() Options { return Options{invoker: func() {}} }`,
				"a/run.go": `package crossseed; func (s *Service) run(ctx context.Context) error { if s.invoker == nil { return nil }; return s.invoker(ctx) }`,
			},
			want: map[string]int{"a/run.go": 1},
		},
		{
			name:  "a hook of a test support package",
			files: map[string]string{"internal/testutil/stub/stub.go": `package stub; type Server struct { OnSync func() }; func (s *Server) sync() { if s.OnSync != nil { s.OnSync() } }`},
			want:  map[string]int{},
		},
		{
			name:  "a nil check on a field that is not a function",
			files: map[string]string{"a/run.go": `package crossseed; func (s *Service) run() bool { return s.store == nil }`},
			want:  map[string]int{},
		},
		{
			name:  "a call of the field without a nil check",
			files: map[string]string{"a/run.go": `package crossseed; func (s *Service) run(ctx context.Context) error { return s.invoker(ctx) }`},
			want:  map[string]int{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := map[string][]byte{"a/service.go": []byte(service)}
			for path, src := range tt.files {
				files[path] = []byte(src)
			}
			got, err := countTestHooks(files)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

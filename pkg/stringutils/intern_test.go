// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package stringutils

import (
	"testing"
)

func TestIntern(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty string", "", ""},
		{"simple string", "hello", "hello"},
		{"with spaces", "hello world", "hello world"},
		{"unicode", "你好世界", "你好世界"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Intern(tt.input)
			if got != tt.want {
				t.Errorf("Intern() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestInternDeduplication(t *testing.T) {
	// Create two separate string allocations with the same content
	s1 := "tracker.example.com"
	s2 := string([]byte("tracker.example.com"))

	// After interning, they should return the same canonical value
	interned1 := Intern(s1)
	interned2 := Intern(s2)

	if interned1 != interned2 {
		t.Errorf("Interned strings should be equal: %q vs %q", interned1, interned2)
	}
}

func TestInternLower(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", ""},
		{"lowercase", "hello", "hello"},
		{"uppercase", "HELLO", "hello"},
		{"mixed", "HeLLo", "hello"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := InternLower(tt.input)
			if got != tt.want {
				t.Errorf("InternLower() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestInternTrimmed(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", ""},
		{"no whitespace", "hello", "hello"},
		{"leading space", "  hello", "hello"},
		{"trailing space", "hello  ", "hello"},
		{"both", "  hello  ", "hello"},
		{"only whitespace", "   ", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := InternTrimmed(tt.input)
			if got != tt.want {
				t.Errorf("InternTrimmed() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestInternNormalized(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", ""},
		{"simple", "hello", "hello"},
		{"uppercase with spaces", "  HELLO  ", "hello"},
		{"mixed case", "HeLLo WoRLd", "hello world"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := InternNormalized(tt.input)
			if got != tt.want {
				t.Errorf("InternNormalized() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMakeHandle(t *testing.T) {
	h1 := MakeHandle("hello")
	h2 := MakeHandle("hello")
	h3 := MakeHandle("world")

	if h1 != h2 {
		t.Error("Handles for same string should be equal")
	}

	if h1 == h3 {
		t.Error("Handles for different strings should not be equal")
	}

	if h1.Value() != "hello" {
		t.Errorf("Handle.Value() = %q, want %q", h1.Value(), "hello")
	}
}

func TestInternAll(t *testing.T) {
	input := []string{"a", "b", "", "c"}
	result := InternAll(input)

	if len(result) != len(input) {
		t.Errorf("InternAll() length = %d, want %d", len(result), len(input))
	}

	for i, s := range result {
		if s != input[i] {
			t.Errorf("InternAll()[%d] = %q, want %q", i, s, input[i])
		}
	}
}

func TestInternAllNormalized(t *testing.T) {
	input := []string{"  A  ", "B", "", "  c  "}
	want := []string{"a", "b", "", "c"}
	result := InternAllNormalized(input)

	if len(result) != len(want) {
		t.Errorf("InternAllNormalized() length = %d, want %d", len(result), len(want))
	}

	for i, s := range result {
		if s != want[i] {
			t.Errorf("InternAllNormalized()[%d] = %q, want %q", i, s, want[i])
		}
	}
}

func TestInternMap(t *testing.T) {
	input := map[int]string{
		1: "a",
		2: "b",
		3: "c",
	}

	result := InternMap(input)

	if len(result) != len(input) {
		t.Errorf("InternMap() length = %d, want %d", len(result), len(input))
	}

	for k, v := range result {
		if v != input[k] {
			t.Errorf("InternMap()[%d] = %q, want %q", k, v, input[k])
		}
	}
}

func TestInternStringMap(t *testing.T) {
	input := map[string]string{
		"category": "movies",
		"tracker":  "example.com",
		"state":    "uploading",
	}

	result := InternStringMap(input)

	if len(result) != len(input) {
		t.Errorf("InternStringMap() length = %d, want %d", len(result), len(input))
	}

	for k, v := range input {
		if result[k] != v {
			t.Errorf("InternStringMap()[%q] = %q, want %q", k, result[k], v)
		}
	}

	// Test empty map
	emptyResult := InternStringMap(nil)
	if emptyResult != nil {
		t.Errorf("InternStringMap(nil) = %v, want nil", emptyResult)
	}

	emptyMap := map[string]string{}
	emptyResult = InternStringMap(emptyMap)
	if len(emptyResult) != 0 {
		t.Errorf("InternStringMap(empty) length = %d, want 0", len(emptyResult))
	}
}

func BenchmarkIntern(b *testing.B) {
	s := "tracker.example.com"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = Intern(s)
	}
}

func BenchmarkInternNormalized(b *testing.B) {
	s := "  TRACKER.EXAMPLE.COM  "
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = InternNormalized(s)
	}
}

func BenchmarkMakeHandle(b *testing.B) {
	s := "tracker.example.com"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = MakeHandle(s)
	}
}

func BenchmarkHandleComparison(b *testing.B) {
	h1 := MakeHandle("tracker.example.com")
	h2 := MakeHandle("tracker.example.com")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = h1 == h2
	}
}

func BenchmarkStringComparison(b *testing.B) {
	s1 := "tracker.example.com"
	s2 := "tracker.example.com"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = s1 == s2
	}
}

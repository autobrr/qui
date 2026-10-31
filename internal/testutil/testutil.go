// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package testutil holds small helpers that tests in several packages share.
package testutil

import (
	"net/http"
	"reflect"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
)

// RoundTripFunc is an http.RoundTripper that answers with the function.
type RoundTripFunc func(*http.Request) (*http.Response, error)

func (f RoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// SetUnexportedField sets a field, exported or not, on the struct that target
// points to, for a test that cannot reach it through a constructor.
func SetUnexportedField(t *testing.T, target any, fieldName string, value any) {
	t.Helper()
	field := reflect.ValueOf(target).Elem().FieldByName(fieldName)
	require.Truef(t, field.IsValid(), "field %s missing on %T", fieldName, target)
	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Set(reflect.ValueOf(value))
}

// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package testutil

import (
	"errors"
	"net/http"
	"os"
	"reflect"
	"runtime"
	"syscall"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
)

func IsWindowsSymlinkPrivilegeError(err error) bool {
	return runtime.GOOS == "windows" && errors.Is(err, syscall.Errno(1314))
}

func SymlinkOrSkip(t testing.TB, target, link string) {
	t.Helper()
	err := os.Symlink(target, link)
	if IsWindowsSymlinkPrivilegeError(err) {
		t.Skip("creating a symlink requires Developer Mode or SeCreateSymbolicLinkPrivilege")
	}
	require.NoError(t, err)
}

type RoundTripFunc func(*http.Request) (*http.Response, error)

func (f RoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func SetUnexportedField(t testing.TB, target any, fieldName string, value any) {
	t.Helper()
	UnexportedField(t, reflect.ValueOf(target).Elem(), fieldName).Set(reflect.ValueOf(value))
}

func UnexportedField(t testing.TB, v reflect.Value, fieldName string) reflect.Value {
	t.Helper()
	field := v.FieldByName(fieldName)
	require.Truef(t, field.IsValid(), "field %s missing on %s", fieldName, v.Type())
	return reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem()
}

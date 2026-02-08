// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"reflect"
	"sync"

	"github.com/moistari/rls"
)

var (
	rlsBitDepthOnce  sync.Once
	rlsBitDepthIndex []int
	rlsHasBitDepth   bool
)

func initRLSBitDepthField() {
	t := reflect.TypeOf(rls.Release{})
	f, ok := t.FieldByName("BitDepth")
	if !ok || f.Type.Kind() != reflect.String {
		rlsHasBitDepth = false
		return
	}

	rlsHasBitDepth = true
	rlsBitDepthIndex = f.Index
}

// rlsBitDepth extracts Release.BitDepth when available.
//
// This is a forward-compat shim: qui may be built against an rls version that
// doesn't yet include BitDepth. Once rls exposes it in a released/tagged version
// (and qui bumps the dependency), this can be replaced with a direct field read.
func rlsBitDepth(r *rls.Release) string {
	if r == nil {
		return ""
	}

	rlsBitDepthOnce.Do(initRLSBitDepthField)
	if !rlsHasBitDepth {
		return ""
	}

	v := reflect.ValueOf(r)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return ""
	}
	v = v.Elem()
	if v.Kind() != reflect.Struct {
		return ""
	}

	f := v.FieldByIndex(rlsBitDepthIndex)
	if !f.IsValid() || f.Kind() != reflect.String {
		return ""
	}

	return f.String()
}

func rlsSetBitDepth(r *rls.Release, bitDepth string) {
	if r == nil || bitDepth == "" {
		return
	}

	rlsBitDepthOnce.Do(initRLSBitDepthField)
	if !rlsHasBitDepth {
		return
	}

	v := reflect.ValueOf(r)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return
	}
	v = v.Elem()
	if v.Kind() != reflect.Struct {
		return
	}

	f := v.FieldByIndex(rlsBitDepthIndex)
	if !f.IsValid() || f.Kind() != reflect.String || !f.CanSet() {
		return
	}

	f.SetString(bitDepth)
}

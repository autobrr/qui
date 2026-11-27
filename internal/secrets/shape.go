// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package secrets

import (
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"reflect"
)

type AnyShape interface {
	spec() shapeSpec
}

type shapeSpec struct {
	typ     reflect.Type
	kind    Kind
	byField bool
	plain   []string
	hide    func(any) any
	marshal func(nested func(Audience) json.Options, a Audience) *json.Marshalers
}

type ShapeInfo struct {
	Type    reflect.Type
	Kind    Kind
	ByField bool
	Plain   []string
	Hide    func(any) any
}

type Shape[T any] struct {
	kind   Kind
	hidden func(T) T
	plain  []string
}

func NewShape[T any](k Kind, hidden func(T) T, plain ...string) Shape[T] {
	return Shape[T]{kind: k, hidden: hidden, plain: plain}
}

func (s Shape[T]) Hidden(a Audience, v T) T {
	if a.sees(s.kind) {
		return v
	}
	return s.hidden(v)
}

func (s Shape[T]) HiddenAll(a Audience, vs []T) []T {
	if a.sees(s.kind) || vs == nil {
		return vs
	}
	hidden := make([]T, len(vs))
	for i, v := range vs {
		hidden[i] = s.hidden(v)
	}
	return hidden
}

func (s Shape[T]) spec() shapeSpec {
	typ := reflect.TypeFor[T]()
	spec := shapeSpec{
		typ:   typ,
		kind:  s.kind,
		plain: s.plain,
		marshal: func(nested func(Audience) json.Options, a Audience) *json.Marshalers {
			inner := a.with(s.kind)
			return json.MarshalToFunc(func(enc *jsontext.Encoder, v T) error {
				return encodeNested(enc, s.hidden(v), nested(inner))
			})
		},
	}
	if typ.Kind() == reflect.Struct {
		spec.byField = true
		spec.hide = func(v any) any { return s.hidden(v.(T)) }
	}
	return spec
}

type RawShape[T any] struct {
	kind       Kind
	hiddenJSON func(T) ([]byte, error)
}

func NewRawShape[T any](k Kind, hiddenJSON func(T) ([]byte, error)) RawShape[T] {
	return RawShape[T]{kind: k, hiddenJSON: hiddenJSON}
}

func (s RawShape[T]) spec() shapeSpec {
	typ := reflect.TypeFor[T]()
	return shapeSpec{
		typ:  typ,
		kind: s.kind,
		marshal: func(func(Audience) json.Options, Audience) *json.Marshalers {
			return json.MarshalToFunc(func(enc *jsontext.Encoder, v T) error {
				hidden, err := s.hiddenJSON(v)
				if err != nil {
					return &hideError{typ: typ, err: err}
				}
				if err := enc.WriteValue(hidden); err != nil {
					return &hideError{typ: typ, err: err}
				}
				return nil
			})
		},
	}
}

type SealedShape[T any] struct {
	kind      Kind
	hiddenFor func(T) (Audience, bool)
}

func NewSealedShape[T any](k Kind, hiddenFor func(T) (Audience, bool)) SealedShape[T] {
	return SealedShape[T]{kind: k, hiddenFor: hiddenFor}
}

func (s SealedShape[T]) spec() shapeSpec {
	return shapeSpec{
		typ:  reflect.TypeFor[T](),
		kind: s.kind,
		marshal: func(nested func(Audience) json.Options, a Audience) *json.Marshalers {
			inner := a.with(s.kind)
			return json.MarshalToFunc(func(enc *jsontext.Encoder, v T) error {
				if hiddenFor, ok := s.hiddenFor(v); !ok || hiddenFor.sees(s.kind) {
					return errors.ErrUnsupported
				}
				return encodeNested(enc, v, nested(inner))
			})
		},
	}
}

func encodeNested(enc *jsontext.Encoder, v any, opts json.Options) error {
	if err := json.MarshalEncode(enc, v, opts); err != nil {
		return &hideError{typ: reflect.TypeOf(v), err: err}
	}
	return nil
}

type hideError struct {
	typ reflect.Type
	err error
}

func (e *hideError) Error() string {
	return "secrets: encode the Hidden form of " + e.typ.String() + ": " + e.err.Error()
}

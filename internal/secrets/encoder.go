// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package secrets

import (
	jsonv1 "encoding/json"
	json "encoding/json/v2"
	"io"
	"iter"
)

type Encoder struct {
	shapes []shapeSpec
	top    [audiences]json.Options
	nested [audiences]json.Options
}

func NewEncoder(shapes ...AnyShape) *Encoder {
	e := &Encoder{shapes: make([]shapeSpec, len(shapes))}
	for i, s := range shapes {
		e.shapes[i] = s.spec()
	}
	checkShapes(e.shapes)
	for kinds := range uint8(audiences) {
		a := Audience{kinds: kinds}
		var marshalers []*json.Marshalers
		for _, s := range e.shapes {
			if !a.sees(s.kind) {
				marshalers = append(marshalers, s.marshal(e.nestedOptions, a))
			}
		}
		e.nested[kinds] = json.WithMarshalers(json.JoinMarshalers(marshalers...))
		e.top[kinds] = jsonv1.DefaultOptionsV1()
		if len(marshalers) > 0 {
			e.top[kinds] = json.JoinOptions(jsonv1.DefaultOptionsV1(), e.nested[kinds])
		}
	}
	return e
}

func (e *Encoder) nestedOptions(a Audience) json.Options {
	return e.nested[a.kinds]
}

func (e *Encoder) Write(w io.Writer, a Audience, v any) error {
	return json.MarshalWrite(w, v, e.top[a.kinds])
}

func (e *Encoder) Marshal(a Audience, v any) ([]byte, error) {
	return json.Marshal(v, e.top[a.kinds])
}

func (e *Encoder) Shapes() iter.Seq[ShapeInfo] {
	return func(yield func(ShapeInfo) bool) {
		for _, s := range e.shapes {
			if !yield(ShapeInfo{Type: s.typ, Kind: s.kind, ByField: s.byField, Plain: s.plain, Hide: s.hide}) {
				return
			}
		}
	}
}

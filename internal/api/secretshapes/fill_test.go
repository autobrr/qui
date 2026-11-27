// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package secretshapes_test

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"unsafe"

	"github.com/autobrr/qui/internal/secrets"
)

type reporter interface {
	Helper()
	Errorf(format string, args ...any)
}

type planted struct {
	name  string
	link  string
	plain bool
}

type filled struct {
	value   any
	planted []planted
}

var stringType = reflect.TypeFor[string]()

func shapesByType(enc *secrets.Encoder) map[reflect.Type]secrets.ShapeInfo {
	shapes := make(map[reflect.Type]secrets.ShapeInfo)
	for info := range enc.Shapes() {
		shapes[info.Type] = info
	}
	return shapes
}

func fill(info secrets.ShapeInfo, shapes map[reflect.Type]secrets.ShapeInfo) filled {
	f := &filler{kind: info.Kind, shapes: shapes, onPath: make(map[reflect.Type]bool)}
	v := reflect.New(info.Type).Elem()
	f.fillValue(v, place{plain: info.Plain, plants: true})
	return filled{value: v.Interface(), planted: f.planted}
}

type filler struct {
	kind    secrets.Kind
	shapes  map[reflect.Type]secrets.ShapeInfo
	onPath  map[reflect.Type]bool
	planted []planted
}

type place struct {
	name      string
	plainName string
	plain     []string
	plants    bool
}

func (p place) field(name string) place {
	p.name, p.plainName = joinName(p.name, name), joinName(p.plainName, name)
	return p
}

func (f *filler) plant(p place) string {
	if !p.plants {
		return ""
	}
	link := "https://planted.invalid/" + p.name + "/made-up-key"
	f.planted = append(f.planted, planted{name: p.name, link: link, plain: slices.Contains(p.plain, p.plainName)})
	return link
}

func (f *filler) enter(v reflect.Value, p place) {
	if info, ok := f.shapes[v.Type()]; ok {
		p.plants = info.Kind == f.kind
		p.plainName, p.plain = "", info.Plain
	}
	f.fillValue(v, p)
}

func (f *filler) fillValue(v reflect.Value, p place) {
	t := v.Type()
	if f.onPath[t] {
		return
	}
	f.onPath[t] = true
	defer delete(f.onPath, t)
	switch t.Kind() {
	case reflect.String:
		if t == stringType && p.name != "" {
			v.SetString(f.plant(p))
		}
	case reflect.Pointer:
		v.Set(reflect.New(t.Elem()))
		f.enter(v.Elem(), p)
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			return
		}
		v.Set(reflect.MakeSlice(t, 1, 1))
		f.enter(v.Index(0), p)
	case reflect.Array:
		if t.Len() > 0 {
			f.enter(v.Index(0), p)
		}
	case reflect.Map:
		key := reflect.New(t.Key()).Elem()
		if t.Key() == stringType {
			key.SetString(f.plant(p))
		}
		elem := reflect.New(t.Elem()).Elem()
		f.enter(elem, p)
		m := reflect.MakeMap(t)
		m.SetMapIndex(key, elem)
		v.Set(m)
	case reflect.Struct:
		f.fillFields(v, p)
	default:
	}
}

func (f *filler) fillFields(v reflect.Value, p place) {
	t := v.Type()
	for i := range t.NumField() {
		field := t.Field(i)
		tag := field.Tag.Get("json")
		if tag == "-" {
			continue
		}
		jsonName, _, _ := strings.Cut(tag, ",")
		fv := v.Field(i)
		if field.Anonymous && jsonName == "" {
			if embedded := promotedStruct(fv); embedded.IsValid() {
				f.fillFields(embedded, p)
				continue
			}
		}
		if field.IsExported() {
			f.enter(fv, p.field(field.Name))
		}
	}
}

func promotedStruct(fv reflect.Value) reflect.Value {
	settable := reflect.NewAt(fv.Type(), unsafe.Pointer(fv.UnsafeAddr())).Elem()
	switch {
	case settable.Kind() == reflect.Struct:
		return settable
	case settable.Kind() == reflect.Pointer && settable.Type().Elem().Kind() == reflect.Struct:
		settable.Set(reflect.New(settable.Type().Elem()))
		return settable.Elem()
	default:
		return reflect.Value{}
	}
}

func joinName(path, name string) string {
	if path == "" {
		return name
	}
	return path + "." + name
}

func encodingJSON(r reporter, v any) string {
	r.Helper()
	out, err := json.Marshal(v)
	if err != nil {
		r.Errorf("encoding/json cannot encode %T: %v", v, err)
	}
	return string(out)
}

func checkHidesEveryString(r reporter, enc *secrets.Encoder) {
	r.Helper()
	shapes := shapesByType(enc)
	for info := range enc.Shapes() {
		if !info.ByField {
			continue
		}
		f := fill(info, shapes)
		encoded, err := enc.Marshal(secrets.Audience{}, f.value)
		if err != nil {
			r.Errorf("%v: the encoder fails with the zero Audience: %v", info.Type, err)
			continue
		}
		reads := map[string]string{
			"the encoder":            string(encoded),
			"Hide and encoding/json": encodingJSON(r, info.Hide(f.value)),
		}
		full := encodingJSON(r, f.value)
		for _, p := range f.planted {
			if !strings.Contains(full, p.link) {
				continue
			}
			for read, out := range reads {
				shows := strings.Contains(out, p.link)
				switch {
				case p.plain && !shows:
					r.Errorf("%v: %s hides the plain field %s", info.Type, read, p.name)
				case !p.plain && shows:
					r.Errorf("%v: %s shows the field %s, which is neither hidden nor listed as plain", info.Type, read, p.name)
				}
			}
		}
	}
}

func checkLeavesSourceWhole(r reporter, enc *secrets.Encoder) {
	r.Helper()
	shapes := shapesByType(enc)
	for info := range enc.Shapes() {
		if !info.ByField {
			continue
		}
		source, before := fill(info, shapes).value, fill(info, shapes).value
		if _, err := enc.Marshal(secrets.Audience{}, source); err != nil {
			r.Errorf("%v: the encoder fails with the zero Audience: %v", info.Type, err)
		}
		if !reflect.DeepEqual(before, source) {
			r.Errorf("%v: the encoder changes its source", info.Type)
		}
		info.Hide(source)
		if !reflect.DeepEqual(before, source) {
			r.Errorf("%v: Hide changes its source", info.Type)
		}
	}
}

func checkFullEncodesAsEncodingJSON(r reporter, enc *secrets.Encoder, others ...any) {
	r.Helper()
	shapes := shapesByType(enc)
	values := slices.Clone(others)
	for info := range enc.Shapes() {
		values = append(values, fill(info, shapes).value)
	}
	for _, v := range values {
		got, err := enc.Marshal(secrets.Full(), v)
		if err != nil {
			r.Errorf("%T: the encoder fails with the Full Audience: %v", v, err)
			continue
		}
		if want := encodingJSON(r, v); string(got) != want {
			r.Errorf("%T: the Full Audience gives %s, encoding/json gives %s", v, got, want)
		}
	}
}

// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package secrets

import (
	"reflect"
	"strings"
)

var stringType = reflect.TypeFor[string]()

func checkShapes(shapes []shapeSpec) {
	shaped := make(map[reflect.Type]bool, len(shapes))
	for _, s := range shapes {
		if shaped[s.typ] {
			panic("secrets: two Shapes of " + s.typ.String())
		}
		shaped[s.typ] = true
	}
	for _, s := range shapes {
		if holdsItself(s.typ) {
			panic("secrets: the Shape type " + s.typ.String() + " can hold a value of its own type")
		}
		names := textNames(s.typ, shaped)
		for _, name := range s.plain {
			if !names[name] {
				panic("secrets: the plain name " + name + " of " + s.typ.String() + " does not end at a text field")
			}
		}
	}
}

func holdsItself(root reflect.Type) bool {
	seen := make(map[reflect.Type]bool)
	var holds func(t reflect.Type) bool
	holds = func(t reflect.Type) bool {
		for _, inner := range innerTypes(t) {
			if inner == root {
				return true
			}
			if seen[inner] {
				continue
			}
			seen[inner] = true
			if holds(inner) {
				return true
			}
		}
		return false
	}
	return holds(root)
}

func innerTypes(t reflect.Type) []reflect.Type {
	switch t.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array:
		return []reflect.Type{t.Elem()}
	case reflect.Map:
		return []reflect.Type{t.Key(), t.Elem()}
	case reflect.Struct:
		var inner []reflect.Type
		for f := range t.Fields() {
			if (f.IsExported() || f.Anonymous) && f.Tag.Get("json") != "-" {
				inner = append(inner, f.Type)
			}
		}
		return inner
	default:
		return nil
	}
}

func textNames(root reflect.Type, shaped map[reflect.Type]bool) map[string]bool {
	names := make(map[string]bool)
	onPath := make(map[reflect.Type]bool)
	var walk func(t reflect.Type, path string)
	enter := func(t reflect.Type, path string) {
		if !shaped[t] {
			walk(t, path)
		}
	}
	walk = func(t reflect.Type, path string) {
		if onPath[t] {
			return
		}
		onPath[t] = true
		defer delete(onPath, t)
		switch t.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Array:
			enter(t.Elem(), path)
		case reflect.Map:
			if t.Key() == stringType {
				names[path] = true
			}
			enter(t.Elem(), path)
		case reflect.String:
			if t == stringType && path != "" {
				names[path] = true
			}
		case reflect.Struct:
			for _, f := range jsonFields(t) {
				enter(f.typ, joinName(path, f.name))
			}
		default:
		}
	}
	walk(root, "")
	return names
}

func joinName(path, name string) string {
	if path == "" {
		return name
	}
	return path + "." + name
}

type jsonField struct {
	name     string
	jsonName string
	typ      reflect.Type
	depth    int
	tagged   bool
}

func jsonFields(t reflect.Type) []jsonField {
	var all []jsonField
	onPath := make(map[reflect.Type]bool)
	var collect func(t reflect.Type, depth int)
	collect = func(t reflect.Type, depth int) {
		if onPath[t] {
			return
		}
		onPath[t] = true
		defer delete(onPath, t)
		for f := range t.Fields() {
			tag := f.Tag.Get("json")
			if tag == "-" {
				continue
			}
			name, _, _ := strings.Cut(tag, ",")
			if f.Anonymous {
				embedded := f.Type
				if embedded.Kind() == reflect.Pointer {
					embedded = embedded.Elem()
				}
				if name == "" && embedded.Kind() == reflect.Struct {
					collect(embedded, depth+1)
					continue
				}
			}
			if !f.IsExported() {
				continue
			}
			jsonName := name
			if jsonName == "" {
				jsonName = f.Name
			}
			all = append(all, jsonField{name: f.Name, jsonName: jsonName, typ: f.Type, depth: depth, tagged: name != ""})
		}
	}
	collect(t, 0)
	return dominantFields(all)
}

func dominantFields(all []jsonField) []jsonField {
	byName := make(map[string][]jsonField)
	for _, f := range all {
		byName[f.jsonName] = append(byName[f.jsonName], f)
	}
	var kept []jsonField
	for _, f := range all {
		rivals := byName[f.jsonName]
		if dominant, ok := dominantField(rivals); ok && dominant == f {
			kept = append(kept, f)
		}
	}
	return kept
}

func dominantField(rivals []jsonField) (jsonField, bool) {
	shallowest := rivals[0].depth
	for _, f := range rivals {
		shallowest = min(shallowest, f.depth)
	}
	var top, tagged []jsonField
	for _, f := range rivals {
		if f.depth != shallowest {
			continue
		}
		top = append(top, f)
		if f.tagged {
			tagged = append(tagged, f)
		}
	}
	switch {
	case len(top) == 1:
		return top[0], true
	case len(tagged) == 1:
		return tagged[0], true
	default:
		return jsonField{}, false
	}
}

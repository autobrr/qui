// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package secrets_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/autobrr/qui/internal/secrets"
)

type state string

type inner struct {
	Name string
}

type named struct {
	Title string `json:"title"`
}

type shadows struct {
	*named
	Title string `json:"title"`
}

type fields struct {
	inner
	Text     string
	Ptr      *string
	List     []string
	Array    [2]string
	ByName   map[string]int
	ByID     map[int]string
	Nested   inner
	Rows     []*inner
	Groups   map[int]inner
	Link     link
	Links    []link
	State    state
	Count    int
	Any      any
	Skipped  string `json:"-"`
	Shadowed shadows
	Deep     deepShadow
	Tagged   taggedRival
}

type deepShadow struct {
	named
	Title int `json:"title"`
}

type labelTag struct {
	Name string `json:"Label"`
}

type labelPlain struct {
	Label int
}

type taggedRival struct {
	labelTag
	labelPlain
}

type nodes map[string]nodes

type holdsNodes struct {
	Rows nodes
}

type tree struct {
	Name     string
	Children []tree
}

type holdsTree struct {
	Tree tree
}

type selfHolder struct {
	Name string
	Next *selfHolder
}

type outerHolder struct {
	Rows []middle
}

type middle struct {
	Back map[string]*outerHolder
}

func shapeOf(plain ...string) secrets.Shape[fields] {
	return secrets.NewShape(secrets.TrackerPasskey, func(f fields) fields { return f }, plain...)
}

func TestNewEncoderAcceptsEachPlainName(t *testing.T) {
	t.Parallel()
	for _, name := range []string{
		"Name", "Text", "Ptr", "List", "Array", "ByName", "ByID",
		"Nested.Name", "Rows.Name", "Groups.Name", "Shadowed.Title", "Tagged.Name",
	} {
		assert.NotPanics(t, func() { secrets.NewEncoder(shapeOf(name), linkShape) }, name)
	}
	assert.NotPanics(t, func() {
		secrets.NewEncoder(secrets.NewShape(secrets.TrackerPasskey, func(h holdsTree) holdsTree { return h }, "Tree.Name"))
	})
	assert.NotPanics(t, func() {
		secrets.NewEncoder(secrets.NewShape(secrets.TrackerPasskey, func(e embedsLink) embedsLink { return e }, "URL", "Note"), linkShape)
	})
	assert.NotPanics(t, func() {
		secrets.NewEncoder(secrets.NewShape(secrets.TrackerPasskey, func(h holdsNodes) holdsNodes { return h }, "Rows"))
	})
}

func TestNewEncoderRefusesAPlainNameThatEndsAtNoTextField(t *testing.T) {
	t.Parallel()
	for _, name := range []string{
		"inner.Name", "Nested", "Rows", "Groups", "State", "Count", "Any", "Skipped",
		"Link.URL", "Link.Note", "Links.Note", "Shadowed.named.Title", "Deep.Title", "Missing", "",
	} {
		assert.Panics(t, func() { secrets.NewEncoder(shapeOf(name), linkShape) }, name)
	}
}

func TestNewEncoderRefusesTwoShapesOfOneType(t *testing.T) {
	t.Parallel()
	raw := secrets.NewRawShape(secrets.NotificationToken, func(link) ([]byte, error) { return nil, nil })
	assert.PanicsWithValue(t, "secrets: two Shapes of secrets_test.link", func() { secrets.NewEncoder(linkShape, raw) })
}

func TestNewEncoderRefusesAShapeTypeThatHoldsItself(t *testing.T) {
	t.Parallel()
	assert.PanicsWithValue(t, "secrets: the Shape type secrets_test.selfHolder can hold a value of its own type", func() {
		secrets.NewEncoder(secrets.NewShape(secrets.TrackerPasskey, func(s selfHolder) selfHolder { return s }))
	})
	assert.PanicsWithValue(t, "secrets: the Shape type secrets_test.outerHolder can hold a value of its own type", func() {
		secrets.NewEncoder(secrets.NewShape(secrets.TrackerPasskey, func(o outerHolder) outerHolder { return o }))
	})
	assert.NotPanics(t, func() {
		secrets.NewEncoder(secrets.NewShape(secrets.TrackerPasskey, func(h holdsTree) holdsTree { return h }))
	})
}

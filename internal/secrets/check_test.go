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
	Array    [2]string
	ByName   map[string]int
	ByID     map[int]string
	Nested   inner
	Rows     []*inner
	Groups   map[int]inner
	Link     link
	State    state
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
		"Array", "ByName", "ByID", "Rows.Name", "Shadowed.Title", "Tagged.Name",
	} {
		assert.NotPanics(t, func() { secrets.NewEncoder(shapeOf(name), linkShape) }, name)
	}
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
		"inner.Name", "Nested", "Rows", "Groups", "State", "Any", "Skipped", "Link.Note", "Deep.Title",
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
	assert.PanicsWithValue(t, "secrets: the Shape type secrets_test.outerHolder can hold a value of its own type", func() {
		secrets.NewEncoder(secrets.NewShape(secrets.TrackerPasskey, func(o outerHolder) outerHolder { return o }))
	})
}

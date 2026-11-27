// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package secrets_test

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/secrets"
)

const (
	zonedLink   = "http://[fe80::1%25eth0]:8080/made-up-key/announce"
	zonedOrigin = "http://[fe80::1%eth0]:8080"
	targetURL   = "discord://made-up-token@channel"
)

type link struct {
	URL  string `json:"url"`
	Note string `json:"note"`
}

type target struct {
	URL  string `json:"url"`
	Link link   `json:"link"`
}

type holder struct {
	Link   link   `json:"link"`
	Links  []link `json:"links"`
	Target target `json:"target"`
}

type methodLink struct{ URL string }

func (m methodLink) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]string{"url": m.URL})
}

type definedLink link

type chain struct {
	URL  string `json:"url"`
	Next any    `json:"next"`
}

type embedsLink struct{ link }

type wrapsWithMethod struct{ L link }

func (w wrapsWithMethod) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct{ L link }{w.L})
}

var (
	linkShape = secrets.NewShape(secrets.TrackerPasskey, func(l link) link {
		l.URL = secrets.Origin(l.URL)
		return l
	}, "Note")
	targetShape = secrets.NewShape(secrets.NotificationToken, func(t target) target {
		t.URL = secrets.Scheme(t.URL)
		return t
	})
	holderShape = secrets.NewShape(secrets.TrackerPasskey, func(h holder) holder {
		h.Link = linkShape.Hidden(secrets.Audience{}, h.Link)
		h.Links = linkShape.HiddenAll(secrets.Audience{}, h.Links)
		h.Target.Link = linkShape.Hidden(secrets.Audience{}, h.Target.Link)
		return h
	})
	methodLinkShape = secrets.NewShape(secrets.TrackerPasskey, func(m methodLink) methodLink {
		m.URL = secrets.Origin(m.URL)
		return m
	})
	chainShape = secrets.NewShape(secrets.TrackerPasskey, func(c chain) chain {
		c.URL = secrets.Origin(c.URL)
		return c
	})
	encoder = secrets.NewEncoder(linkShape, targetShape, holderShape, methodLinkShape, chainShape)
)

func encodingJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return string(b)
}

func encode(t *testing.T, enc *secrets.Encoder, a secrets.Audience, v any) string {
	t.Helper()
	b, err := enc.Marshal(a, v)
	require.NoError(t, err)
	var buf bytes.Buffer
	require.NoError(t, enc.Write(&buf, a, v))
	require.Equal(t, string(b), buf.String())
	return string(b)
}

func TestEncoderAppliesAShapeAtAnyDepth(t *testing.T) {
	t.Parallel()
	full := link{URL: zonedLink, Note: "plain"}
	hidden := link{URL: zonedOrigin, Note: "plain"}
	for _, tc := range []struct {
		name string
		in   any
		want any
	}{
		{"the value", full, hidden},
		{"a slice", []link{full}, []link{hidden}},
		{"a map value", map[string]link{"a": full}, map[string]link{"a": hidden}},
		{"behind a pointer", &full, &hidden},
		{"inside an any", []any{full}, []any{hidden}},
		{"inside a Shape of another kind", target{URL: targetURL, Link: full}, target{URL: "discord://", Link: hidden}},
		{"a type with its own MarshalJSON", methodLink{URL: zonedLink}, map[string]string{"url": zonedOrigin}},
		{"once below a Shape of the same kind", holder{Link: full, Links: []link{full}, Target: target{URL: targetURL, Link: full}}, holder{Link: hidden, Links: []link{hidden}, Target: target{URL: "discord://", Link: hidden}}},
		{"a nil slice and a nil map", struct {
			Links  []link
			ByName map[string]link
		}{}, jsontext.Value(`{"Links":null,"ByName":null}`)},
		{"not for an embedding type", embedsLink{full}, embedsLink{full}},
		{"not for a defined type", definedLink(full), definedLink(full)},
		{"not inside a type whose own MarshalJSON encodes it", wrapsWithMethod{full}, struct{ L link }{full}},
		{"not inside raw JSON", jsontext.Value(encodingJSON(t, full)), full},
		{"not for its own type behind an interface field", chain{URL: zonedLink, Next: chain{URL: zonedLink}}, chain{URL: zonedOrigin, Next: chain{URL: zonedLink}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, encodingJSON(t, tc.want), encode(t, encoder, secrets.Audience{}, tc.in))
		})
	}
}

func TestEncoderHidesEachValueOnceForEachAudience(t *testing.T) {
	t.Parallel()
	full := holder{Link: link{URL: zonedLink}, Links: []link{{URL: zonedLink}}, Target: target{URL: targetURL, Link: link{URL: zonedLink}}}
	before := encodingJSON(t, full)
	trackersHidden := holder{Link: link{URL: zonedOrigin}, Links: []link{{URL: zonedOrigin}}, Target: target{URL: targetURL, Link: link{URL: zonedOrigin}}}
	for _, tc := range []struct {
		name     string
		audience secrets.Audience
		want     holder
	}{
		{"zero", secrets.Audience{}, func() holder { h := trackersHidden; h.Target.URL = "discord://"; return h }()},
		{"trackers only", secrets.NewAudience(secrets.TrackerPasskey), func() holder { h := full; h.Target.URL = "discord://"; return h }()},
		{"notifications only", secrets.NewAudience(secrets.NotificationToken), trackersHidden},
		{"full", secrets.Full(), full},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := encode(t, encoder, tc.audience, full)
			assert.Equal(t, encodingJSON(t, tc.want), got)
			assert.Equal(t, before, encodingJSON(t, full))

			viaHidden := holderShape.Hidden(tc.audience, full)
			viaHidden.Target = targetShape.Hidden(tc.audience, viaHidden.Target)
			assert.Equal(t, got, encodingJSON(t, viaHidden))
		})
	}
}

func TestHiddenHidesOnlyItsKind(t *testing.T) {
	t.Parallel()
	full := target{URL: targetURL, Link: link{URL: zonedLink}}
	assert.Equal(t, target{URL: "discord://", Link: link{URL: zonedLink}}, targetShape.Hidden(secrets.Audience{}, full))
	assert.Equal(t, full, targetShape.Hidden(secrets.NewAudience(secrets.NotificationToken), full))
}

func TestHiddenAll(t *testing.T) {
	t.Parallel()
	full := []link{{URL: zonedLink}, {URL: "** [DHT] **"}}
	hidden := linkShape.HiddenAll(secrets.Audience{}, full)
	assert.Equal(t, []link{{URL: zonedOrigin}, {URL: "** [DHT] **"}}, hidden)
	assert.Equal(t, link{URL: zonedLink}, full[0])
	assert.Nil(t, linkShape.HiddenAll(secrets.Audience{}, nil))
	assert.Equal(t, []link{}, linkShape.HiddenAll(secrets.Audience{}, []link{}))
}

func TestHiddenAllocatesNothingForAReaderThatSeesTheKind(t *testing.T) {
	v := holder{Link: link{URL: zonedLink}, Links: []link{{URL: zonedLink}}}
	links := v.Links
	allocs := testing.AllocsPerRun(1000, func() {
		v = holderShape.Hidden(secrets.Full(), v)
		v = holderShape.Hidden(secrets.NewAudience(secrets.TrackerPasskey), v)
		links = linkShape.HiddenAll(secrets.Full(), links)
	})
	assert.Zero(t, allocs)
}

func TestFullEncodesAsEncodingJSON(t *testing.T) {
	t.Parallel()
	for _, v := range []any{
		holder{Link: link{URL: zonedLink, Note: "<b> & é  "}, Target: target{URL: targetURL}},
		map[string]any{"b": []int{1}, "a": "<script>", "c": nil},
		[]string(nil),
		jsontext.Value(`{"a" : "<x>"}`),
	} {
		assert.Equal(t, encodingJSON(t, v), encode(t, encoder, secrets.Full(), v))
	}
}

type sealedList struct {
	Rows      []link `json:"rows"`
	hiddenFor secrets.Audience
	hidden    bool
}

var sealedShape = secrets.NewSealedShape(secrets.TrackerPasskey, func(l *sealedList) (secrets.Audience, bool) {
	return l.hiddenFor, l.hidden
})

func TestSealedShape(t *testing.T) {
	t.Parallel()
	enc := secrets.NewEncoder(sealedShape, linkShape)
	once := []link{{URL: zonedOrigin}}
	full := []link{{URL: zonedLink}}

	assert.Equal(t, encodingJSON(t, sealedList{Rows: once}), encode(t, enc, secrets.Audience{}, &sealedList{Rows: once, hidden: true}))
	assert.Equal(t, encodingJSON(t, struct{ Rows []link }{once}), encode(t, enc, secrets.Audience{}, struct{ Rows []link }{full}))
	assert.Equal(t, encodingJSON(t, sealedList{Rows: once}), encode(t, enc, secrets.Audience{}, &sealedList{Rows: full}))
	assert.Equal(t, encodingJSON(t, sealedList{Rows: once}), encode(t, enc, secrets.Audience{}, &sealedList{Rows: full, hiddenFor: secrets.Full(), hidden: true}))
	assert.Equal(t, encodingJSON(t, sealedList{Rows: full}), encode(t, enc, secrets.Full(), &sealedList{Rows: full}))
}

type rawNames map[string]json.RawMessage

func TestRawShapeWritesTheBytesOfEncodingJSON(t *testing.T) {
	t.Parallel()
	hide := func(names rawNames) ([]byte, error) {
		hidden := make(rawNames, len(names))
		for name, value := range names {
			hidden[secrets.StandIn(name)] = value
		}
		return json.Marshal(hidden)
	}
	rawStruct := secrets.NewRawShape(secrets.TrackerPasskey, func(l link) ([]byte, error) {
		l.URL = secrets.Origin(l.URL)
		return json.Marshal(l)
	})
	enc := secrets.NewEncoder(secrets.NewRawShape(secrets.TrackerPasskey, hide), rawStruct)
	names := rawNames{"https://tracker.example/rss/made-up-key": json.RawMessage(`{"title":"<b>é & ü </b>"}`)}
	hiddenNames, err := hide(names)
	require.NoError(t, err)

	assert.Equal(t, encodingJSON(t, json.RawMessage(hiddenNames)), encode(t, enc, secrets.Audience{}, names))
	assert.Equal(t, encodingJSON(t, names), encode(t, enc, secrets.Full(), names))
	note := link{URL: zonedLink, Note: "<i>ö &  </i>"}
	assert.Equal(t, encodingJSON(t, []link{{URL: zonedOrigin, Note: note.Note}}), encode(t, enc, secrets.Audience{}, []link{note}))
}

func TestRawShapeErrorFailsTheWholeEncode(t *testing.T) {
	t.Parallel()
	for _, failure := range []error{
		errors.New("cannot read the value"),
		errors.ErrUnsupported,
		fmt.Errorf("wrapped: %w", errors.ErrUnsupported),
	} {
		t.Run(failure.Error(), func(t *testing.T) {
			t.Parallel()
			enc := secrets.NewEncoder(secrets.NewRawShape(secrets.TrackerPasskey, func(link) ([]byte, error) { return nil, failure }))
			value := struct {
				Before string
				Link   link
			}{"shown before the failure", link{URL: zonedLink}}

			out, err := enc.Marshal(secrets.Audience{}, value)
			require.Error(t, err)
			assert.Nil(t, out)
			require.NotErrorIs(t, err, errors.ErrUnsupported)
			require.Error(t, enc.Write(&bytes.Buffer{}, secrets.Audience{}, value))

			assert.Equal(t, encodingJSON(t, value), encode(t, enc, secrets.Full(), value))
		})
	}
}

func TestShapesListsEachShape(t *testing.T) {
	t.Parallel()
	raw := secrets.NewRawShape(secrets.NotificationToken, func(rawNames) ([]byte, error) { return nil, nil })
	enc := secrets.NewEncoder(linkShape, raw, sealedShape)
	infos := slices.Collect(enc.Shapes())
	require.Len(t, infos, 3)

	assert.Equal(t, reflect.TypeFor[link](), infos[0].Type)
	assert.Equal(t, secrets.TrackerPasskey, infos[0].Kind)
	assert.True(t, infos[0].ByField)
	assert.Equal(t, []string{"Note"}, infos[0].Plain)
	assert.Equal(t, link{URL: zonedOrigin}, infos[0].Hide(link{URL: zonedLink}))

	assert.Equal(t, reflect.TypeFor[rawNames](), infos[1].Type)
	assert.Equal(t, secrets.NotificationToken, infos[1].Kind)
	assert.False(t, infos[1].ByField)
	assert.Nil(t, infos[1].Hide)

	assert.Equal(t, reflect.TypeFor[*sealedList](), infos[2].Type)
	assert.False(t, infos[2].ByField)
}

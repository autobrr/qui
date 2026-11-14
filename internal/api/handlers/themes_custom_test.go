// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/services/activity"
)

const okThemeCSS = "/* @name: Ocean */\n:root { --background: red; }\n.dark { --background: blue; }\n"

type customThemeRig struct {
	dir    string
	pub    *recordingPublisher
	router http.Handler
}

func newCustomThemeRig(t *testing.T, premium bool) *customThemeRig {
	t.Helper()
	dir := t.TempDir()
	pub := &recordingPublisher{}
	h := NewThemesHandler(stubThemesDir{dir: dir}, stubPremium{ok: premium}, nil, nil, pub)
	r := chi.NewRouter()
	r.Get("/api/themes/custom", h.ListCustomThemes)
	r.Post("/api/themes/custom", h.CreateCustomTheme)
	r.Put("/api/themes/custom/{id}", h.ReplaceCustomTheme)
	r.Delete("/api/themes/custom/{id}", h.DeleteCustomTheme)
	return &customThemeRig{dir: dir, pub: pub, router: r}
}

func (rig *customThemeRig) do(t *testing.T, method, target string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var payload string
	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		payload = string(raw)
	}
	req := httptest.NewRequestWithContext(t.Context(), method, target, strings.NewReader(payload))
	rec := httptest.NewRecorder()
	rig.router.ServeHTTP(rec, req)
	return rec
}

func (rig *customThemeRig) create(t *testing.T, id, css string) *httptest.ResponseRecorder {
	t.Helper()
	return rig.do(t, http.MethodPost, "/api/themes/custom", map[string]string{"id": id, "css": css})
}

func (rig *customThemeRig) replace(t *testing.T, id, css string) *httptest.ResponseRecorder {
	t.Helper()
	return rig.do(t, http.MethodPut, "/api/themes/custom/"+id, map[string]string{"css": css})
}

func (rig *customThemeRig) remove(t *testing.T, id string) *httptest.ResponseRecorder {
	t.Helper()
	return rig.do(t, http.MethodDelete, "/api/themes/custom/"+id, nil)
}

func (rig *customThemeRig) write(t *testing.T, name, css string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(rig.dir, name), []byte(css), 0o600))
}

func (rig *customThemeRig) read(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(rig.dir, name))
	require.NoError(t, err)
	return string(raw)
}

func (rig *customThemeRig) fileNames(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(rig.dir)
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func errorMessage(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var resp ErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp), rec.Body.String())
	return resp.Error
}

func requireThemeEvents(t *testing.T, pub *recordingPublisher, n int) {
	t.Helper()
	require.Len(t, pub.events, n)
	for _, ev := range pub.events {
		require.Equal(t, activity.KindCustomThemes, ev.Kind)
	}
}

func TestCustomTheme_CreateReplaceRemove(t *testing.T) {
	rig := newCustomThemeRig(t, true)

	rec := rig.create(t, "ocean", okThemeCSS)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var created CustomTheme
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	require.Equal(t, CustomTheme{ID: "ocean", Filename: "ocean.css", CSS: okThemeCSS}, created)
	require.Equal(t, okThemeCSS, rig.read(t, "ocean.css"))
	requireThemeEvents(t, rig.pub, 1)

	edited := strings.Replace(okThemeCSS, "red", "green", 1)
	rec = rig.replace(t, "ocean", edited)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var replaced CustomTheme
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &replaced))
	require.Equal(t, CustomTheme{ID: "ocean", Filename: "ocean.css", CSS: edited}, replaced)
	require.Equal(t, edited, rig.read(t, "ocean.css"))
	requireThemeEvents(t, rig.pub, 2)

	rec = rig.remove(t, "ocean")
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	require.Empty(t, rig.fileNames(t), "remove leaves no file and no temporary file behind")
	requireThemeEvents(t, rig.pub, 3)
}

func TestCustomTheme_WritesNeedLicense(t *testing.T) {
	rig := newCustomThemeRig(t, false)
	rig.write(t, "ocean.css", okThemeCSS)

	for name, rec := range map[string]*httptest.ResponseRecorder{
		"create":  rig.create(t, "sunset", okThemeCSS),
		"replace": rig.replace(t, "ocean", "x"),
		"remove":  rig.remove(t, "ocean"),
	} {
		require.Equal(t, http.StatusForbidden, rec.Code, name)
	}
	require.Equal(t, []string{"ocean.css"}, rig.fileNames(t))
	require.Equal(t, okThemeCSS, rig.read(t, "ocean.css"))
	require.Empty(t, rig.pub.events)
}

func TestCustomTheme_CreateConflictIgnoresCase(t *testing.T) {
	rig := newCustomThemeRig(t, true)
	rig.write(t, "Ocean.CSS", "hand made")

	rec := rig.create(t, "ocean", okThemeCSS)

	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	require.Equal(t, "hand made", rig.read(t, "Ocean.CSS"))
	require.Equal(t, []string{"Ocean.CSS"}, rig.fileNames(t))
	require.Empty(t, rig.pub.events)

	// The editor answers a 409 with a replace of the same id.
	rec = rig.replace(t, "ocean", okThemeCSS)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, okThemeCSS, rig.read(t, "Ocean.CSS"))
}

func TestCustomTheme_UnknownIDIsNotFound(t *testing.T) {
	rig := newCustomThemeRig(t, true)
	rig.write(t, "ocean.css", okThemeCSS)

	require.Equal(t, http.StatusNotFound, rig.replace(t, "sunset", okThemeCSS).Code)
	require.Equal(t, http.StatusNotFound, rig.remove(t, "sunset").Code)
	require.Equal(t, []string{"ocean.css"}, rig.fileNames(t))
	require.Empty(t, rig.pub.events)
}

func TestCustomTheme_CreateRefusesID(t *testing.T) {
	for _, id := range []string{
		"",
		"Ocean",
		"-ocean",
		"_ocean",
		"oce an",
		"ocean.css",
		"../ocean",
		"a/b",
		`a\b`,
		"ö",
		strings.Repeat("a", 65),
	} {
		t.Run(id, func(t *testing.T) {
			rig := newCustomThemeRig(t, true)
			rec := rig.create(t, id, okThemeCSS)
			require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			require.Empty(t, rig.fileNames(t))
			require.Empty(t, rig.pub.events)
		})
	}

	rig := newCustomThemeRig(t, true)
	for _, id := range []string{"a", "0-theme_2", strings.Repeat("a", 64)} {
		require.Equal(t, http.StatusCreated, rig.create(t, id, okThemeCSS).Code, id)
	}
}

func TestCustomTheme_EditsAndRemovesNameThatCreateRefuses(t *testing.T) {
	rig := newCustomThemeRig(t, true)
	rig.write(t, "My Theme.css", "hand made")
	rig.write(t, "Other.css", "other")

	rec := rig.replace(t, "My%20Theme", okThemeCSS)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, okThemeCSS, rig.read(t, "My Theme.css"))

	rec = rig.remove(t, "Other")
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	require.Equal(t, []string{"My Theme.css"}, rig.fileNames(t))
}

func TestCustomTheme_EscapedIDReachesFile(t *testing.T) {
	rig := newCustomThemeRig(t, true)
	rig.write(t, "My Theme (dark).css", "hand made")
	rig.write(t, "it's mine.css", "hand made")

	rec := rig.replace(t, "My%20Theme%20(dark)", okThemeCSS)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, okThemeCSS, rig.read(t, "My Theme (dark).css"))

	rec = rig.remove(t, "it's%20mine")
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	require.Equal(t, []string{"My Theme (dark).css"}, rig.fileNames(t))
}

func TestCustomTheme_PercentInFileNameIsNotDecodedTwice(t *testing.T) {
	rig := newCustomThemeRig(t, true)
	rig.write(t, "100%.css", "hundred")
	rig.write(t, "a%20b.css", "escaped name")
	rig.write(t, "a b.css", "space name")

	rec := rig.replace(t, "100%25", okThemeCSS)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, okThemeCSS, rig.read(t, "100%.css"))

	rec = rig.remove(t, "a%2520b")
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	require.Equal(t, []string{"100%.css", "a b.css"}, rig.fileNames(t))
	require.Equal(t, "space name", rig.read(t, "a b.css"))
}

func TestCustomTheme_ConcurrentCreatesOfOneID(t *testing.T) {
	rig := newCustomThemeRig(t, true)
	const callers = 8
	start := make(chan struct{})
	codes := make(chan int, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wg.Go(func() {
			<-start
			codes <- rig.create(t, "ocean", fmt.Sprintf(":root{--a:%d}.dark{--a:%d}", i, i)).Code
		})
	}
	close(start)
	wg.Wait()
	close(codes)

	counts := map[int]int{}
	for code := range codes {
		counts[code]++
	}
	require.Equal(t, map[int]int{http.StatusCreated: 1, http.StatusConflict: callers - 1}, counts)
	requireThemeEvents(t, rig.pub, 1)
}

func TestCustomTheme_PathOutsideFolder(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "themes")
	require.NoError(t, os.Mkdir(dir, 0o750))
	outside := filepath.Join(parent, "secret.css")
	require.NoError(t, os.WriteFile(outside, []byte("secret"), 0o600))

	h := NewThemesHandler(stubThemesDir{dir: dir}, stubPremium{ok: true}, nil, nil, &recordingPublisher{})
	r := chi.NewRouter()
	r.Put("/api/themes/custom/{id}", h.ReplaceCustomTheme)
	r.Delete("/api/themes/custom/{id}", h.DeleteCustomTheme)

	for _, id := range []string{"..%2Fsecret", "..%5Csecret", "%2E%2E%2Fsecret", "..", "%2Fsecret"} {
		for _, method := range []string{http.MethodPut, http.MethodDelete} {
			req := httptest.NewRequestWithContext(t.Context(), method, "/api/themes/custom/"+id, strings.NewReader(`{"css":"x"}`))
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			require.NotEqual(t, http.StatusOK, rec.Code, "%s %s", method, id)
			require.NotEqual(t, http.StatusNoContent, rec.Code, "%s %s", method, id)
		}
	}
	raw, err := os.ReadFile(outside)
	require.NoError(t, err)
	require.Equal(t, "secret", string(raw))
}

func TestCustomTheme_SymlinkIsNotTouched(t *testing.T) {
	rig := newCustomThemeRig(t, true)
	target := filepath.Join(t.TempDir(), "target.css")
	require.NoError(t, os.WriteFile(target, []byte("target"), 0o600))
	err := os.Symlink(target, filepath.Join(rig.dir, "linked.css"))
	if runtime.GOOS == "windows" && errors.Is(err, syscall.Errno(1314)) {
		t.Skipf("symlink creation requires Developer Mode or SeCreateSymbolicLinkPrivilege: %v", err)
	}
	require.NoError(t, err)

	require.Equal(t, http.StatusNotFound, rig.replace(t, "linked", okThemeCSS).Code)
	require.Equal(t, http.StatusNotFound, rig.remove(t, "linked").Code)
	require.Equal(t, http.StatusConflict, rig.create(t, "linked", okThemeCSS).Code)

	raw, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, "target", string(raw))
	link, err := os.Lstat(filepath.Join(rig.dir, "linked.css"))
	require.NoError(t, err)
	require.NotZero(t, link.Mode()&os.ModeSymlink, "the symlink stays a symlink")
	require.Empty(t, rig.pub.events)
}

func TestCustomTheme_SizeLimit(t *testing.T) {
	rig := newCustomThemeRig(t, true)
	big := ":root{--a:b}.dark{--a:b}" + strings.Repeat(" ", maxCustomThemeFileSize)
	atLimit := big[:maxCustomThemeFileSize]

	require.Equal(t, http.StatusBadRequest, rig.create(t, "big", big[:maxCustomThemeFileSize+1]).Code)
	require.Equal(t, http.StatusCreated, rig.create(t, "edge", atLimit).Code)
	require.Equal(t, http.StatusBadRequest, rig.replace(t, "edge", big[:maxCustomThemeFileSize+1]).Code)
	require.Equal(t, atLimit, rig.read(t, "edge.css"))
	requireThemeEvents(t, rig.pub, 1)
}

func TestCustomTheme_FileCountLimit(t *testing.T) {
	rig := newCustomThemeRig(t, true)
	for i := range maxCustomThemeFiles - 1 {
		rig.write(t, fmt.Sprintf("theme-%03d.css", i), okThemeCSS)
	}

	require.Equal(t, http.StatusCreated, rig.create(t, "last", okThemeCSS).Code)
	rec := rig.create(t, "one-more", okThemeCSS)
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	require.Len(t, rig.fileNames(t), maxCustomThemeFiles)
	requireThemeEvents(t, rig.pub, 1)
}

func TestCustomTheme_UnwritableFolderNamesFolder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a read-only directory mode does not stop a write on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root writes to a read-only directory")
	}
	rig := newCustomThemeRig(t, true)
	require.NoError(t, os.Chmod(rig.dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(rig.dir, 0o700) })

	rec := rig.create(t, "ocean", okThemeCSS)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Contains(t, errorMessage(t, rec), rig.dir)
	require.Empty(t, rig.pub.events)
}

func TestCustomTheme_OutsideFileRule(t *testing.T) {
	refused := []struct {
		name   string
		css    string
		line   int
		reason string
	}{
		{"import", "@import 'https://example.invalid/a.css';", 1, "@import"},
		{"import in uppercase", "\n@IMPORT url(x.css);", 2, "@import"},
		{"image-set", ":root{}\n.dark{}\nbody { background: image-set('a.png' 1x); }", 3, "image-set()"},
		{"http url", "body {\n  background: url(https://example.invalid/a.png);\n}", 2, "url()"},
		{"quoted http url", "body { background: url( \"https://example.invalid/a.png\" ); }", 1, "url()"},
		{"protocol-relative url", "body { background: url(//example.invalid/a.png); }", 1, "url()"},
		{"url in uppercase", "\n\n\nbody { background: URL(https://example.invalid/a.png); }", 4, "url()"},
		{"relative url", "body { background: url(a.png); }", 1, "url()"},
		{"escaped url", "body { background: \\75rl(https://example.invalid/a.png); }", 1, "backslash"},
		{"escaped import", "@\\69mport 'x.css';", 1, "backslash"},
		{"line break in a string", "body::after { content: \"a\n@import 'x.css'; b\"; }", 1, "line break"},
		{"url after a comment", "/* one\n two */ body { background: url(http://example.invalid); }", 2, "url()"},
	}
	for _, tt := range refused {
		t.Run(tt.name, func(t *testing.T) {
			rig := newCustomThemeRig(t, true)
			rec := rig.create(t, "ocean", tt.css)
			require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			msg := errorMessage(t, rec)
			require.Contains(t, msg, fmt.Sprintf("line %d", tt.line))
			require.Contains(t, msg, tt.reason)
			require.Empty(t, rig.fileNames(t))
			require.Empty(t, rig.pub.events)
		})
	}

	allowed := []struct {
		name string
		css  string
	}{
		{"url in a comment", "/* background: url(https://example.invalid/a.png); @import 'x'; \\75rl() */ :root { --a: b; }"},
		{"data url", "body { background: url(data:image/png;base64,iVBORw0KGgo=); }"},
		{"quoted data url", "body { background: url( 'DATA:image/svg+xml,<svg></svg>' ); }"},
		{"fragment url", "body { filter: url(#blur); }"},
		{"url text in a string", "body::after { content: \"url(https://example.invalid) @import \\\" \"; }"},
		{"backslash in a string", "body::after { content: \"\\2014\"; }"},
		{"font from data", "@font-face { font-family: X; src: url(\"data:font/woff2;base64,AAAA\") format(\"woff2\"); }"},
	}
	for _, tt := range allowed {
		t.Run(tt.name, func(t *testing.T) {
			rig := newCustomThemeRig(t, true)
			rec := rig.create(t, "ocean", tt.css)
			require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		})
	}
}

func TestCustomTheme_ReplaceAppliesOutsideFileRule(t *testing.T) {
	rig := newCustomThemeRig(t, true)
	rig.write(t, "ocean.css", "body { background: url(https://example.invalid/a.png); }")

	rec := rig.replace(t, "ocean", "body { background: url(https://example.invalid/b.png); }")

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, errorMessage(t, rec), "line 1")
	require.Contains(t, rig.read(t, "ocean.css"), "a.png", "the hand-made file stays")
	require.Empty(t, rig.pub.events)

	list := rig.do(t, http.MethodGet, "/api/themes/custom", nil)
	require.Equal(t, http.StatusOK, list.Code)
	require.Contains(t, list.Body.String(), "a.png", "the list route serves a hand-made file with a url()")
}

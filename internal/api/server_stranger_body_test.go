// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/auth"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

const (
	strangerBodyLimit = 1 << 20
	strangerBodySize  = 8 << 20
)

var strangerBodies = []struct {
	name, contentType, head, tail string
}{
	{"JSON", "application/json", `{"pad":"`, `"}`},
	{"form", "application/x-www-form-urlencoded", "pad=", ""},
	{
		"multipart", "multipart/form-data; boundary=qui",
		"--qui\r\nContent-Disposition: form-data; name=\"pad\"; filename=\"pad\"\r\n\r\n",
		"\r\n--qui--\r\n",
	},
}

type padding struct{}

func (padding) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'a'
	}
	return len(p), nil
}

type countedBody struct {
	io.Reader

	read int64
}

func (b *countedBody) Read(p []byte) (int, error) {
	n, err := b.Reader.Read(p)
	b.read += int64(n)
	return n, err
}

func (*countedBody) Close() error { return nil }

func TestRoutesReadAtMostOneMiBOfABodyWithoutSignIn(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*testing.T, *Dependencies)
	}{
		{"after setup", func(*testing.T, *Dependencies) {}},
		{"before setup", func(t *testing.T, deps *Dependencies) {
			deps.AuthService = auth.NewService(testdb.NewMigratedSQLite(t, "stranger-body-before-setup"))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deps := newTestDependencies(t)
			tc.setup(t, deps)
			router, err := NewServer(deps).Handler()
			require.NoError(t, err)

			var routes []routeKey
			require.NoError(t, chi.Walk(router, func(method, path string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
				routes = append(routes, routeKey{method, path})
				return nil
			}))
			require.GreaterOrEqual(t, len(routes), 260)

			for _, route := range routes {
				path := strings.ReplaceAll(routeParam.ReplaceAllString(route.Path, "1"), "*", "1")
				for _, shape := range strangerBodies {
					body := &countedBody{Reader: io.MultiReader(
						strings.NewReader(shape.head),
						io.LimitReader(padding{}, strangerBodySize),
						strings.NewReader(shape.tail),
					)}
					req := httptest.NewRequestWithContext(t.Context(), route.Method, path, body)
					req.ContentLength = int64(len(shape.head)) + strangerBodySize + int64(len(shape.tail))
					req.Header.Set("Content-Type", shape.contentType)
					req.Header.Set("X-Requested-With", "XMLHttpRequest")
					rec := httptest.NewRecorder()

					router.ServeHTTP(rec, req)

					require.LessOrEqual(t, body.read, int64(strangerBodyLimit+1),
						"%s %s read %d bytes of a %s body without sign-in", route.Method, route.Path, body.read, shape.name)
					require.NotEqual(t, handlerPanicBody, rec.Body.String(), "%s %s: the handler panicked", route.Method, route.Path)
				}
			}
		})
	}
}

// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package jackett

import (
	"net/http"
	"os"
	"testing"
)

// roundTripOnly hides CloseIdleConnections from httptest.Server.Close.
// Close calls it on http.DefaultTransport, which every client in this package
// shares. On a response without a body, the transport puts the connection back
// in the idle pool before it hands the response over, so a parallel test that
// closes its server can break that request with
// "http: CloseIdleConnections called".
type roundTripOnly struct{ http.RoundTripper }

func TestMain(m *testing.M) {
	http.DefaultTransport = roundTripOnly{http.DefaultTransport}
	os.Exit(m.Run())
}

// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package jackett

import (
	"net/http"
	"os"
	"testing"
)

type roundTripOnly struct{ http.RoundTripper }

func TestMain(m *testing.M) {
	http.DefaultTransport = roundTripOnly{http.DefaultTransport}
	os.Exit(m.Run())
}

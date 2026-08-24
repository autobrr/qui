// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package middleware

import (
	"net/http"
	"runtime/debug"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"

	"github.com/autobrr/qui/pkg/redact"
)

var slowRequestThreshold = time.Second

// Logger logs HTTP requests using a local zerolog logger
func Logger(logger zerolog.Logger) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		fn := func(w http.ResponseWriter, r *http.Request) {
			l := logger.With().Logger()

			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			t1 := time.Now()
			defer func() {
				t2 := time.Now()

				// Recover and record stack traces in case of a panic
				if rec := recover(); rec != nil {
					l.Error().
						Str("type", "error").
						Interface("recover_info", rec).
						Bytes("debug_stack", debug.Stack()).
						Msg("log system error")
					http.Error(ww, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				}

				// Server errors and slow requests carry the evidence a report
				// needs, so they surface at the default level. Healthy traffic
				// stays at TRACE.
				latency := t2.Sub(t1)
				level := zerolog.TraceLevel
				if ww.Status() >= http.StatusInternalServerError || latency > slowRequestThreshold {
					level = zerolog.DebugLevel
				}

				// Guarded so the redaction and header lookups are skipped when off.
				if e := l.WithLevel(level); e.Enabled() {
					e.Str("type", "access").
						Str("remote_ip", r.RemoteAddr).
						Str("url", redact.ProxyPath(r.URL.EscapedPath())).
						Str("proto", r.Proto).
						Str("method", r.Method).
						Str("user_agent", r.Header.Get("User-Agent")).
						Int("status", ww.Status()).
						Float64("latency_ms", float64(latency.Nanoseconds())/1000000.0).
						Str("bytes_in", r.Header.Get("Content-Length")).
						Int("bytes_out", ww.BytesWritten()).
						Msg("incoming_request")
				}
			}()

			next.ServeHTTP(ww, r)
		}
		return http.HandlerFunc(fn)
	}
}

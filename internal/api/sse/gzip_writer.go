// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sse

import (
	"compress/gzip"
	"net/http"
	"strconv"
	"strings"
)

// gzipLevel trades a little CPU for ~10% fewer bytes than the REST middleware's
// level 2. The stream pays it once per init snapshot rather than per request, and
// on a 460 KB snapshot both levels finish in about 2 ms.
const gzipLevel = 5

// gzipSessionWriter compresses an SSE session. It sits above bufferedSessionWriter
// so the deadline path is untouched: Serve builds its http.ResponseController from
// the raw ResponseWriter before any wrapping, and Unwrap keeps flushSession and
// initFlushError walking down to the buffered writer.
//
// Flush order matters. gzip.Writer buffers, so a flush that skips the compressor
// leaves the event sitting in the deflate window and the client sees nothing until
// something later fills it. Flush therefore ends the deflate block first, then lets
// the buffered writer hand the finished frame to the socket (init phase) or to the
// drain goroutine (buffered phase) as one queued message.
type gzipSessionWriter struct {
	sw *bufferedSessionWriter
	gz *gzip.Writer
}

func newGzipSessionWriter(sw *bufferedSessionWriter) *gzipSessionWriter {
	// NewWriterLevel only errors on an invalid level, and gzipLevel is a constant.
	gz, _ := gzip.NewWriterLevel(sw, gzipLevel)
	return &gzipSessionWriter{sw: sw, gz: gz}
}

func (w *gzipSessionWriter) Header() http.Header { return w.sw.Header() }

func (w *gzipSessionWriter) WriteHeader(code int) { w.sw.WriteHeader(code) }

func (w *gzipSessionWriter) Write(p []byte) (int, error) { return w.gz.Write(p) }

func (w *gzipSessionWriter) Unwrap() http.ResponseWriter { return w.sw }

func (w *gzipSessionWriter) Flush() {
	// End the deflate block before flushing the socket, or the event stays in the
	// compressor. A flush error means the session is finished anyway: the next write
	// fails and drops this client.
	_ = w.gz.Flush()
	w.sw.Flush()
}

// acceptsGzip reports whether the client asked for gzip. A gzip token with q=0 is
// an explicit refusal, which is how a reverse proxy turns compression off, so it
// must not be read as support.
func acceptsGzip(header string) bool {
	for part := range strings.SplitSeq(header, ",") {
		token, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		if !strings.EqualFold(strings.TrimSpace(token), "gzip") {
			continue
		}
		key, value, ok := strings.Cut(strings.TrimSpace(params), "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(key), "q") {
			return true
		}
		q, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		return err != nil || q > 0
	}
	return false
}

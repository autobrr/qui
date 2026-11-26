// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sse

import (
	"compress/gzip"
	"net/http"
	"strconv"
	"strings"
)

const gzipLevel = 5

type gzipSessionWriter struct {
	sw *bufferedSessionWriter
	gz *gzip.Writer
}

func newGzipSessionWriter(sw *bufferedSessionWriter) *gzipSessionWriter {
	gz, _ := gzip.NewWriterLevel(sw, gzipLevel)
	return &gzipSessionWriter{sw: sw, gz: gz}
}

func (w *gzipSessionWriter) Header() http.Header { return w.sw.Header() }

func (w *gzipSessionWriter) WriteHeader(code int) { w.sw.WriteHeader(code) }

func (w *gzipSessionWriter) Write(p []byte) (int, error) { return w.gz.Write(p) }

func (w *gzipSessionWriter) Unwrap() http.ResponseWriter { return w.sw }

func (w *gzipSessionWriter) Flush() {
	_ = w.gz.Flush()
	w.sw.Flush()
}

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

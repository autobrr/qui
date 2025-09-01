// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package middleware

import (
	"compress/flate"
	"compress/gzip"
	"io"
	"net/http"
	"strings"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
)

// CompressionAlgorithm represents supported compression algorithms
type CompressionAlgorithm int

const (
	AlgorithmNone CompressionAlgorithm = iota
	AlgorithmGzip
	AlgorithmBrotli
	AlgorithmZstd
	AlgorithmDeflate
)

// compressionWriter wraps an http.ResponseWriter to handle different compression algorithms
type compressionWriter struct {
	http.ResponseWriter
	algorithm              CompressionAlgorithm
	writer                 io.Writer
	size                   int
	minSize                int
	baseLevel              int
	currentLevel           int
	wroteHeader            bool
	compressionInitialized bool
}

// CompressionLevelConfig defines compression levels for different size ranges
type CompressionLevelConfig struct {
	SmallFileLevel  int // For files < 10KB (lower compression, faster - minimal wire benefit)
	MediumFileLevel int // For files 10KB-100KB (balanced)
	LargeFileLevel  int // For files > 100KB (higher compression, slower but better wire savings)
}

// DefaultCompressionLevels provides sensible defaults
var DefaultCompressionLevels = CompressionLevelConfig{
	SmallFileLevel:  3, // Lower compression for small files (faster)
	MediumFileLevel: 4, // Balanced for medium files
	LargeFileLevel:  6, // Higher compression for large files (better ratio)
}

func (w *compressionWriter) Write(data []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}

	// Track size
	w.size += len(data)

	// If we haven't initialized compression yet and we're above threshold, initialize it
	if !w.compressionInitialized && w.size >= w.minSize {
		if w.shouldCompress() {
			if err := w.initCompression(); err != nil {
				// Fallback to uncompressed
				w.writer = w.ResponseWriter
				w.compressionInitialized = true
			}
		} else {
			w.writer = w.ResponseWriter
			w.compressionInitialized = true
		}
	}

	// If still no writer, use direct response writer
	if w.writer == nil {
		w.writer = w.ResponseWriter
	}

	return w.writer.Write(data)
}

func (w *compressionWriter) WriteHeader(code int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true

	// Don't set Content-Length if we might compress
	if w.size == 0 { // Haven't written anything yet
		w.Header().Del("Content-Length")
	}

	w.ResponseWriter.WriteHeader(code)
}

func (w *compressionWriter) shouldCompress() bool {
	contentType := w.Header().Get("Content-Type")
	return strings.Contains(contentType, "text/") ||
		strings.Contains(contentType, "application/json") ||
		strings.Contains(contentType, "application/xml") ||
		strings.Contains(contentType, "application/javascript")
}

func (w *compressionWriter) getCompressionLevel() int {
	// Dynamic compression level based on response size
	// Larger files benefit more from higher compression levels due to wire time savings
	switch {
	case w.size < 10*1024: // < 10KB - small files
		if w.baseLevel-2 < 1 {
			return 1
		}
		return w.baseLevel - 2 // Lower compression for small files (faster, minimal wire benefit)
	case w.size < 100*1024: // 10KB - 100KB - medium files
		return w.baseLevel // Base level for medium files (balanced)
	default: // > 100KB - large files
		if w.baseLevel+2 > 9 {
			return 9
		}
		return w.baseLevel + 2 // Higher compression for large files (slower but much better wire savings)
	}
}

func (w *compressionWriter) initCompression() error {
	level := w.getCompressionLevel()
	w.currentLevel = level

	switch w.algorithm {
	case AlgorithmZstd:
		w.Header().Set("Content-Encoding", "zstd")
		encoder, err := zstd.NewWriter(w.ResponseWriter, zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(level)))
		if err != nil {
			return err
		}
		w.writer = &zstdWriter{writer: encoder}

	case AlgorithmBrotli:
		w.Header().Set("Content-Encoding", "br")
		w.writer = brotli.NewWriterLevel(w.ResponseWriter, level)

	case AlgorithmGzip:
		w.Header().Set("Content-Encoding", "gzip")
		var err error
		w.writer, err = gzip.NewWriterLevel(w.ResponseWriter, level)
		if err != nil {
			return err
		}

	case AlgorithmDeflate:
		w.Header().Set("Content-Encoding", "deflate")
		w.writer, _ = flate.NewWriter(w.ResponseWriter, level)
	}

	w.compressionInitialized = true
	return nil
}

func (w *compressionWriter) Flush() {
	if flusher, ok := w.writer.(http.Flusher); ok {
		flusher.Flush()
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *compressionWriter) Close() error {
	if closer, ok := w.writer.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

// zstdWriter wraps zstd encoder to handle closing
type zstdWriter struct {
	writer *zstd.Encoder
}

func (w *zstdWriter) Write(data []byte) (int, error) {
	return w.writer.Write(data)
}

func (w *zstdWriter) Flush() {
	w.writer.Flush()
}

func (w *zstdWriter) Close() error {
	return w.writer.Close()
}

// negotiateAlgorithm determines the best compression algorithm based on client preferences
func negotiateAlgorithm(acceptEncoding string, preferZstd, preferBrotli bool) CompressionAlgorithm {
	// Parse quality values from Accept-Encoding header
	encodings := parseAcceptEncoding(acceptEncoding)

	// Priority order: Zstd > Brotli > Gzip > Deflate > None
	if preferZstd && encodings["zstd"] > 0 {
		return AlgorithmZstd
	}
	if preferBrotli && encodings["br"] > 0 {
		return AlgorithmBrotli
	}
	if encodings["gzip"] > 0 {
		return AlgorithmGzip
	}
	if encodings["deflate"] > 0 {
		return AlgorithmDeflate
	}

	return AlgorithmNone
}

// parseAcceptEncoding parses the Accept-Encoding header and returns quality values
func parseAcceptEncoding(acceptEncoding string) map[string]float64 {
	encodings := make(map[string]float64)

	if acceptEncoding == "" {
		return encodings
	}

	parts := strings.Split(acceptEncoding, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Handle quality values (e.g., "gzip;q=0.8")
		var encoding string
		var qvalue float64 = 1.0

		if idx := strings.Index(part, ";q="); idx != -1 {
			encoding = strings.TrimSpace(part[:idx])
			if len(part) > idx+3 {
				if q, err := parseQualityValue(part[idx+3:]); err == nil {
					qvalue = q
				}
			}
		} else {
			encoding = part
		}

		// Handle special cases
		switch encoding {
		case "*":
			// Wildcard - accept any encoding with default quality
			encodings["gzip"] = 1.0
			encodings["br"] = 1.0
			encodings["zstd"] = 1.0
			encodings["deflate"] = 1.0
		case "deflate":
			// Handle deflate separately from gzip
			encodings["deflate"] = qvalue
		default:
			encodings[encoding] = qvalue
		}
	}

	return encodings
}

// parseQualityValue parses a quality value (q=0.8)
func parseQualityValue(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 1.0, nil
	}

	// Simple float parsing
	switch s {
	case "1", "1.0":
		return 1.0, nil
	case "0.9":
		return 0.9, nil
	case "0.8":
		return 0.8, nil
	case "0.7":
		return 0.7, nil
	case "0.6":
		return 0.6, nil
	case "0.5":
		return 0.5, nil
	case "0.4":
		return 0.4, nil
	case "0.3":
		return 0.3, nil
	case "0.2":
		return 0.2, nil
	case "0.1":
		return 0.1, nil
	case "0", "0.0":
		return 0.0, nil
	default:
		return 1.0, nil // Default to 1.0 for unrecognized values
	}
}

// SelectiveCompress returns a middleware that compresses responses above a minimum size
// using the best available algorithm based on client preferences
func SelectiveCompress(minSize, level int, preferZstd, preferBrotli bool) func(http.Handler) http.Handler {
	if level < 1 {
		level = 1
	}
	if level > 9 {
		level = 9
	}
	if minSize < 0 {
		minSize = 1024 // Default 1KB
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Negotiate the best compression algorithm
			algorithm := negotiateAlgorithm(r.Header.Get("Accept-Encoding"), preferZstd, preferBrotli)

			if algorithm == AlgorithmNone {
				next.ServeHTTP(w, r)
				return
			}

			// Wrap the response writer
			wrapped := &compressionWriter{
				ResponseWriter: w,
				algorithm:      algorithm,
				minSize:        minSize,
				baseLevel:      level,
			}

			// Add Vary header to indicate response varies based on Accept-Encoding
			w.Header().Set("Vary", "Accept-Encoding")

			next.ServeHTTP(wrapped, r)

			// Close compression writer if it was initialized
			if wrapped.writer != nil {
				wrapped.Close()
			}
		})
	}
}

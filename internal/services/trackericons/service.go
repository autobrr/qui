// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package trackericons

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	_ "github.com/mat/besticon/v3/ico"
	"github.com/rs/zerolog/log"
	"golang.org/x/image/draw"
	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
	"golang.org/x/sync/singleflight"
	"golang.org/x/text/transform"

	"github.com/autobrr/qui/pkg/httphelpers"
)

const (
	iconDirName        = "tracker-icons"
	maxHTMLBytes int64 = 2 << 20 // 2 MiB
	maxIconBytes int64 = 1 << 20 // 1 MiB

	fetchTimeout    = 15 * time.Second
	failureCooldown = 30 * time.Minute
)

var (
	// ErrIconNotFound is returned when no icon could be fetched for a tracker.
	ErrIconNotFound = errors.New("tracker icon not found")
	// ErrInvalidTrackerHost is returned when the requested tracker host is invalid.
	ErrInvalidTrackerHost = errors.New("invalid tracker host")
	// ErrFetchingDisabled is returned when tracker icon fetching is disabled by configuration.
	ErrFetchingDisabled = errors.New("tracker icon fetching is disabled")

	globalService *Service
	globalMu      sync.RWMutex
	fetchEnabled  atomic.Bool
)

func init() {
	fetchEnabled.Store(true)
}

// Service handles fetching and caching tracker icons on disk.
type Service struct {
	iconDir string
	client  *http.Client
	ua      string

	group singleflight.Group

	failureMu   sync.Mutex
	lastFailure map[string]time.Time
}

// Flow overview:
//   * tracker syncs call QueueFetch when they encounter a host; the helper skips
//     work if a PNG already exists on disk or the host is still in cooldown.
//   * GetIcon handles the actual fetch pipeline with singleflight so only one
//     goroutine ever attempts a download for a given host at a time.
//   * fetchAndStoreIcon probes a bounded set of candidate URLs, deduplicates
//     them, attempts each icon once, and stops after the first success. Images
//     are decoded (falling back to ICO when needed), resized, and written
//     atomically.
//   * ListIcons simply streams whatever PNGs already live on disk; the frontend
//     polls that endpoint but never triggers new fetch attempts.
//   * Example: encountering "cdn.trackers.example.org" produces host
//     candidates `["cdn.trackers.example.org", "trackers.example.org",
//     "example.org"]`. For each we probe `https://…/` and `http://…/`, scrape
//     any `<link rel="icon">` references, append `/favicon.ico`, and stop at
//     the first icon that decodes successfully.

// NewService creates a new tracker icon service rooted in the provided data directory.
func NewService(dataDir, userAgent string) (*Service, error) {
	if strings.TrimSpace(dataDir) == "" {
		return nil, fmt.Errorf("data directory must be provided")
	}

	iconDir := filepath.Join(dataDir, iconDirName)
	if err := os.MkdirAll(iconDir, 0o755); err != nil {
		return nil, fmt.Errorf("create tracker icon directory: %w", err)
	}

	svc := &Service{
		iconDir:     iconDir,
		client:      &http.Client{Timeout: fetchTimeout},
		lastFailure: make(map[string]time.Time),
	}

	if trimmed := strings.TrimSpace(userAgent); trimmed != "" {
		svc.ua = trimmed
	} else {
		svc.ua = "qui/dev"
	}

	if err := svc.preloadIconsFromDisk(); err != nil {
		return nil, err
	}

	return svc, nil
}

func SetGlobal(svc *Service) {
	globalMu.Lock()
	globalService = svc
	globalMu.Unlock()
}

func SetFetchEnabled(enabled bool) {
	fetchEnabled.Store(enabled)
}

func QueueFetch(host, trackerURL string) {
	globalMu.RLock()
	svc := globalService
	globalMu.RUnlock()

	if svc == nil {
		return
	}

	if !fetchEnabled.Load() {
		return
	}

	sanitized := sanitizeHost(host)
	if sanitized == "" {
		return
	}

	// Check if already cached
	path := svc.iconPath(sanitized)
	if _, err := os.Stat(path); err == nil {
		return
	}

	// Check cooldown
	if !svc.canAttempt(sanitized) {
		return
	}

	// Queue background fetch
	go func(h string, tracker string) {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()
		if _, err := svc.GetIcon(ctx, h, tracker); err != nil {
			// Intentionally ignore errors here; they are tracked internally for cooldown.
		}
	}(sanitized, trackerURL)
}

// ListIcons returns all cached tracker icons as base64-encoded data URLs.
func (s *Service) ListIcons(ctx context.Context) (map[string]string, error) {
	entries, err := os.ReadDir(s.iconDir)
	if err != nil {
		return nil, fmt.Errorf("read icon directory: %w", err)
	}

	icons := make(map[string]string)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".png") {
			continue
		}

		iconPath := filepath.Join(s.iconDir, entry.Name())
		data, err := os.ReadFile(iconPath)
		if err != nil {
			continue
		}

		// Extract tracker name from filename (remove .png extension)
		trackerName := strings.TrimSuffix(entry.Name(), ".png")
		encoded := base64.StdEncoding.EncodeToString(data)
		dataURL := "data:image/png;base64," + encoded
		icons[trackerName] = dataURL
		if after, ok := strings.CutPrefix(trackerName, "www."); ok {
			trimmed := after
			if trimmed != "" {
				if _, exists := icons[trimmed]; !exists {
					icons[trimmed] = dataURL
				}
			}
		}
	}

	return icons, nil
}

// GetIcon ensures an icon is available for the host (fetching if necessary) and returns the file path.
func (s *Service) GetIcon(ctx context.Context, host, trackerURL string) (string, error) {
	sanitized := sanitizeHost(host)
	if sanitized == "" {
		return "", ErrInvalidTrackerHost
	}

	iconPath := s.iconPath(sanitized)
	if _, err := os.Stat(iconPath); err == nil {
		return iconPath, nil
	}

	if !fetchEnabled.Load() {
		log.Trace().Str("host", host).Msg("Icon fetch skipped: remote fetching disabled")
		return "", ErrFetchingDisabled
	}

	ch := s.group.DoChan(sanitized, func() (any, error) {
		if _, err := os.Stat(iconPath); err == nil {
			return iconPath, nil
		}

		if !s.canAttempt(sanitized) {
			return "", ErrIconNotFound
		}

		err := s.fetchAndStoreIcon(ctx, sanitized, trackerURL)
		if err != nil {
			if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
				s.recordFailure(sanitized)
			}
			if errors.Is(err, ErrIconNotFound) {
				return "", ErrIconNotFound
			}
			return "", fmt.Errorf("fetch tracker icon: %w", err)
		}

		s.clearFailure(sanitized)
		return iconPath, nil
	})

	select {
	case result := <-ch:
		if result.Err != nil {
			return "", result.Err
		}
		path, ok := result.Val.(string)
		if !ok {
			return "", ErrIconNotFound
		}
		return path, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// iconPath returns the on-disk path for the tracker icon.
func (s *Service) iconPath(host string) string {
	filename := safeFilename(host) + ".png"
	return filepath.Join(s.iconDir, filename)
}

// fetchAndStoreIcon attempts to download, normalise, and cache the icon for the given host.
func (s *Service) fetchAndStoreIcon(ctx context.Context, host, trackerURL string) error {
	ctx, cancel := ensureContext(ctx)
	defer cancel()

	iconPath := s.iconPath(host)
	attempted := make(map[string]struct{})
	hostCandidates := generateHostCandidates(host)
	if len(hostCandidates) == 0 {
		hostCandidates = []string{sanitizeHost(host)}
	}

	for idx, candidateHost := range hostCandidates {
		candidateTrackerURL := trackerURL
		if idx > 0 {
			candidateTrackerURL = ""
		}

		baseURLs := s.buildBaseCandidates(candidateHost, candidateTrackerURL)
		if len(baseURLs) == 0 {
			continue
		}

		localSeen := make(map[string]struct{})
		var iconURLs []string

		for _, baseURL := range baseURLs {
			urls, err := s.discoverIcons(ctx, baseURL)
			if err == nil {
				for _, iconURL := range urls {
					if _, ok := attempted[iconURL]; ok {
						continue
					}
					if _, ok := localSeen[iconURL]; ok {
						continue
					}
					localSeen[iconURL] = struct{}{}
					iconURLs = append(iconURLs, iconURL)
				}
			}

			fallback := baseURL.ResolveReference(&url.URL{Path: "/favicon.ico"}).String()
			if _, ok := attempted[fallback]; !ok {
				if _, seen := localSeen[fallback]; !seen {
					localSeen[fallback] = struct{}{}
					iconURLs = append(iconURLs, fallback)
				}
			}
		}

		for _, iconURL := range iconURLs {
			attempted[iconURL] = struct{}{}

			data, contentType, err := s.fetchIconBytes(ctx, iconURL)
			if err != nil {
				continue
			}

			img, err := decodeImage(data, contentType, iconURL)
			if err != nil {
				continue
			}

			resized := resizeToSquare(img, 16)
			if err := s.writePNG(resized, iconPath); err != nil {
				return err
			}

			return nil
		}
	}

	return ErrIconNotFound
}

func (s *Service) discoverIcons(ctx context.Context, baseURL *url.URL) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", s.ua)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer httphelpers.DrainAndClose(resp)
	limitedReader := io.LimitReader(resp.Body, maxHTMLBytes)

	if resp.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	rawHTML, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, err
	}

	encoding, _, _ := charset.DetermineEncoding(rawHTML, resp.Header.Get("Content-Type"))
	utf8Reader := transform.NewReader(bytes.NewReader(rawHTML), encoding.NewDecoder())

	document, err := html.Parse(utf8Reader)
	if err != nil {
		return nil, err
	}

	var icons []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && strings.EqualFold(n.Data, "link") {
			var rel, href string
			for _, attr := range n.Attr {
				switch strings.ToLower(attr.Key) {
				case "rel":
					rel = attr.Val
				case "href":
					href = attr.Val
				}
			}

			if href != "" && rel != "" && strings.Contains(strings.ToLower(rel), "icon") {
				if resolved, err := baseURL.Parse(href); err == nil {
					icons = append(icons, resolved.String())
				}
			}
		}

		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}

	walk(document)
	return icons, nil
}
func (s *Service) fetchIconBytes(ctx context.Context, iconURL string) ([]byte, string, error) {
	if strings.HasPrefix(iconURL, "data:") {
		return parseDataURI(iconURL)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, iconURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", s.ua)
	req.Header.Set("Accept", "image/*")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer httphelpers.DrainAndClose(resp)

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxIconBytes))
	if err != nil {
		return nil, "", err
	}

	if resp.StatusCode >= http.StatusBadRequest {
		return nil, "", fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	return data, contentType, nil
}

func (s *Service) writePNG(img image.Image, path string) error {
	// Encode in memory first - fails fast without any disk I/O
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return fmt.Errorf("encode png: %w", err)
	}

	// Create temp file for atomic write
	tmpFile, err := os.CreateTemp(s.iconDir, "tracker-icon-*.png")
	if err != nil {
		return err
	}
	tmpName := tmpFile.Name()
	tmpFile.Close()

	// Cleanup on failure
	var success bool
	defer func() {
		if !success {
			os.Remove(tmpName)
		}
	}()

	// Write complete buffer atomically
	if err := os.WriteFile(tmpName, buf.Bytes(), 0o644); err != nil {
		return err
	}

	// Ensure final permissions are 0644 regardless of CreateTemp defaults
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return err
	}

	// Atomic rename to final location
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}

	success = true
	return nil
}

func (s *Service) buildBaseCandidates(host, trackerURL string) []*url.URL {
	host = sanitizeHost(host)
	if host == "" {
		return nil
	}

	seen := make(map[string]struct{})
	var ordered []string
	add := func(raw string) {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" {
			return
		}
		u.Path = "/"
		u.RawQuery = ""
		u.Fragment = ""
		key := u.String()
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		ordered = append(ordered, key)
	}

	if trackerURL != "" {
		if u, err := url.Parse(trackerURL); err == nil {
			switch strings.ToLower(u.Scheme) {
			case "http", "https":
				add((&url.URL{Scheme: u.Scheme, Host: u.Host}).String())
			default:
				if u.Host != "" {
					add((&url.URL{Scheme: "https", Host: u.Host}).String())
					add((&url.URL{Scheme: "http", Host: u.Host}).String())
				}
			}
		}
	}

	add((&url.URL{Scheme: "https", Host: host}).String())
	add((&url.URL{Scheme: "http", Host: host}).String())

	var urls []*url.URL
	for _, raw := range ordered {
		if u, err := url.Parse(raw); err == nil {
			urls = append(urls, u)
		}
	}

	return urls
}

func (s *Service) canAttempt(host string) bool {
	s.failureMu.Lock()
	defer s.failureMu.Unlock()

	if ts, ok := s.lastFailure[host]; ok {
		if time.Since(ts) < failureCooldown {
			return false
		}
	}

	return true
}

func (s *Service) recordFailure(host string) {
	s.failureMu.Lock()
	s.lastFailure[host] = time.Now()
	s.failureMu.Unlock()
}

func (s *Service) clearFailure(host string) {
	s.failureMu.Lock()
	delete(s.lastFailure, host)
	s.failureMu.Unlock()
}

func sanitizeHost(host string) string {
	host = strings.TrimSpace(host)
	host = strings.Trim(host, "/")
	if host == "" {
		return ""
	}

	// url.Parse treats values without a scheme as paths; prefix with // to coerce host parsing.
	parsed, err := url.Parse(host)
	if err != nil || parsed.Host == "" {
		parsed, err = url.Parse("//" + host)
		if err != nil {
			return ""
		}
	}

	hostname := strings.Trim(strings.ToLower(parsed.Hostname()), ".")
	if hostname == "" {
		return ""
	}

	if strings.Contains(hostname, ":") {
		return "[" + hostname + "]"
	}

	return hostname
}

// safeFilename normalises a host into a filesystem-friendly base name.
func safeFilename(host string) string {
	sanitized := sanitizeHost(host)
	if sanitized == "" {
		return "_invalid_"
	}

	name := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' {
			return r
		}
		if r >= '0' && r <= '9' {
			return r
		}
		switch r {
		case '.', '-', '_':
			return r
		default:
			return '_'
		}
	}, sanitized)

	name = strings.Trim(name, "._-")
	if name == "" {
		return "_invalid_"
	}

	return name
}

func generateHostCandidates(host string) []string {
	sanitized := sanitizeHost(host)
	if sanitized == "" {
		return nil
	}

	seen := make(map[string]struct{})
	var candidates []string

	add := func(candidate string) {
		candidate = sanitizeHost(candidate)
		if candidate == "" {
			return
		}
		if _, exists := seen[candidate]; exists {
			return
		}
		seen[candidate] = struct{}{}
		candidates = append(candidates, candidate)
	}

	add(sanitized)

	current := sanitized
	for {
		next := trimLeadingLabel(current)
		if next == "" || next == current {
			break
		}
		if strings.Count(next, ".") == 0 {
			break
		}
		add(next)
		current = next
		if strings.Count(current, ".") == 1 {
			break
		}
	}

	if !strings.HasPrefix(sanitized, "www.") && strings.Count(sanitized, ".") == 1 {
		add("www." + sanitized)
	}

	return candidates
}

func trimLeadingLabel(host string) string {
	idx := strings.Index(host, ".")
	if idx == -1 {
		return host
	}
	if idx+1 >= len(host) {
		return ""
	}
	return host[idx+1:]
}

func ensureContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		return context.WithTimeout(context.Background(), fetchTimeout)
	}

	if deadline, ok := ctx.Deadline(); ok {
		if time.Until(deadline) < fetchTimeout {
			return context.WithCancel(ctx)
		}
	}

	return context.WithTimeout(ctx, fetchTimeout)
}

func parseDataURI(dataURI string) ([]byte, string, error) {
	withoutScheme := strings.TrimPrefix(dataURI, "data:")
	parts := strings.SplitN(withoutScheme, ",", 2)
	if len(parts) != 2 {
		return nil, "", fmt.Errorf("invalid data URI")
	}

	meta := parts[0]
	payload := parts[1]

	contentType := ""
	if idx := strings.Index(meta, ";"); idx >= 0 {
		contentType = meta[:idx]
		meta = meta[idx+1:]
	} else if meta != "" {
		contentType = meta
		meta = ""
	}

	if !strings.Contains(meta, "base64") {
		return nil, "", fmt.Errorf("unsupported data URI encoding")
	}

	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return nil, "", err
	}

	return decoded, contentType, nil
}

func decodeImage(data []byte, contentType, originalURL string) (image.Image, error) {
	if strings.Contains(strings.ToLower(contentType), "svg") || strings.HasSuffix(strings.ToLower(originalURL), ".svg") {
		return nil, fmt.Errorf("svg icons are not supported")
	}

	const maxDimension = 1024
	validateDimensions := func(width, height int) error {
		switch {
		case width < 1 || height < 1:
			return fmt.Errorf("icon dimensions invalid: %dx%d (min 1)", width, height)
		case width > maxDimension || height > maxDimension:
			return fmt.Errorf("icon dimensions too large: %dx%d (max %d)", width, height, maxDimension)
		default:
			return nil
		}
	}

	reader := bytes.NewReader(data)

	// Check dimensions before expensive decode to avoid decompression bombs
	// If validation succeeds here we can skip re-checking the same dimensions later.
	cfg, _, err := image.DecodeConfig(reader)
	if err != nil {
		return nil, err
	}

	if err := validateDimensions(cfg.Width, cfg.Height); err != nil {
		return nil, err
	}

	// Reset reader for full decode
	if _, err := reader.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("failed to reset reader: %w", err)
	}

	img, _, err := image.Decode(reader)
	return img, err
}

func resizeToSquare(src image.Image, size int) image.Image {
	if src == nil {
		return nil
	}
	bounds := src.Bounds()
	if bounds.Dx() == size && bounds.Dy() == size {
		return src
	}

	dst := image.NewNRGBA(image.Rect(0, 0, size, size))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, bounds, draw.Src, nil)
	return dst
}

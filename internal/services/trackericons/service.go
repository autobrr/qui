// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package trackericons

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json/v2"
	"errors"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"maps"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"code.dny.dev/ssrf"
	"github.com/mat/besticon/v3/ico"
	"github.com/rs/zerolog/log"
	"golang.org/x/image/draw"
	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
	"golang.org/x/net/http/httpproxy"
	"golang.org/x/net/idna"
	"golang.org/x/sync/singleflight"
	"golang.org/x/text/transform"

	"github.com/autobrr/qui/internal/services/activity"
	"github.com/autobrr/qui/pkg/httphelpers"
)

const (
	iconDirName            = "tracker-icons"
	failuresFileName       = "fetch-failures.json"
	maxHTMLBytes     int64 = 2 << 20
	maxIconBytes     int64 = 1 << 20

	fetchTimeout   = 15 * time.Second
	requestTimeout = 5 * time.Second

	baseFailureCooldown = 30 * time.Minute
	maxFailureCooldown  = 24 * time.Hour
)

var (
	ErrIconNotFound       = errors.New("tracker icon not found")
	ErrInvalidTrackerHost = errors.New("invalid tracker host")
	ErrFetchingDisabled   = errors.New("tracker icon fetching is disabled")

	globalService *Service
	globalMu      sync.RWMutex
	fetchEnabled  atomic.Bool
)

var _ = []func(io.Reader) (image.Image, error){gif.Decode, jpeg.Decode, ico.Decode}

func init() {
	fetchEnabled.Store(true)
}

type Service struct {
	iconDir string
	client  *http.Client
	ua      string

	group singleflight.Group

	failureMu sync.Mutex
	failures  map[string]failureState

	activityPublisher activity.Publisher
}

type failureState struct {
	Attempts    int       `json:"attempts"`
	LastFailure time.Time `json:"lastFailure"`
}

func NewService(dataDir, userAgent string) (*Service, error) {
	if strings.TrimSpace(dataDir) == "" {
		return nil, errors.New("data directory must be provided")
	}

	iconDir := filepath.Join(dataDir, iconDirName)
	if err := os.MkdirAll(iconDir, 0o755); err != nil {
		return nil, fmt.Errorf("create tracker icon directory: %w", err)
	}

	svc := &Service{
		iconDir:           iconDir,
		client:            &http.Client{Timeout: fetchTimeout, Transport: newPublicOnlyTransport()},
		failures:          make(map[string]failureState),
		activityPublisher: activity.NopPublisher{},
	}

	if trimmed := strings.TrimSpace(userAgent); trimmed != "" {
		svc.ua = trimmed
	} else {
		svc.ua = "qui/dev"
	}

	if err := svc.preloadIconsFromDisk(); err != nil {
		return nil, err
	}

	if err := svc.loadFailures(); err != nil {
		log.Warn().Err(err).Msg("Tracker icon fetch failures could not be loaded; starting with a clean slate")
	}

	return svc, nil
}

var publicOnly = ssrf.New(
	ssrf.WithAnyPort(),
	ssrf.WithAllowedV6Prefixes(ssrf.IPv6NAT64Prefix),
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func newPublicOnlyTransport() http.RoundTripper {
	proxyFunc := httpproxy.FromEnvironment().ProxyFunc()

	direct := http.DefaultTransport.(*http.Transport).Clone()
	direct.Proxy = nil
	direct.DialContext = (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second, Control: safeDial}).DialContext

	proxied := http.DefaultTransport.(*http.Transport).Clone()
	proxied.Proxy = func(r *http.Request) (*url.URL, error) { return proxyFunc(r.URL) }

	return roundTripFunc(func(r *http.Request) (*http.Response, error) {
		proxyURL, err := proxyFunc(r.URL)
		if err != nil {
			return nil, err
		}
		if proxyURL == nil {
			return direct.RoundTrip(r)
		}
		if err := checkPublicHost(r.Context(), r.URL.Hostname()); err != nil {
			return nil, err
		}
		return proxied.RoundTrip(r)
	})
}

func checkPublicHost(ctx context.Context, host string) error {
	if ascii, err := idna.Lookup.ToASCII(host); err == nil {
		host = ascii
	}
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return err
	}
	for _, ip := range ips {
		ip = ip.Unmap()
		if err := publicOnly.SafeAddr(ip); err != nil {
			return err
		}
		if err := checkNAT64(ip); err != nil {
			return err
		}
	}
	return nil
}

func safeDial(network, address string, c syscall.RawConn) error {
	if err := publicOnly.Safe(network, address, c); err != nil {
		return err
	}
	return checkNAT64(netip.MustParseAddrPort(address).Addr())
}

func checkNAT64(ip netip.Addr) error {
	if !ssrf.IPv6NAT64Prefix.Contains(ip) {
		return nil
	}
	b := ip.As16()
	return publicOnly.SafeAddr(netip.AddrFrom4([4]byte(b[12:])))
}

func (s *Service) SetActivityPublisher(publisher activity.Publisher) {
	if s == nil || publisher == nil {
		return
	}
	s.activityPublisher = publisher
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

	path := svc.iconPath(sanitized)
	if _, err := os.Stat(path); err == nil {
		return
	}

	if !svc.canAttempt(sanitized) {
		return
	}

	go func(h string, tracker string) {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()
		_, _ = svc.GetIcon(ctx, h, tracker)
	}(sanitized, trackerURL)
}

func (s *Service) ListIcons(ctx context.Context) (map[string]string, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	entries, err := os.ReadDir(s.iconDir)
	if err != nil {
		return nil, fmt.Errorf("read icon directory: %w", err)
	}

	icons := make(map[string]string)
	for _, entry := range entries {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		ext := filepath.Ext(name)
		if strings.ToLower(ext) != ".png" {
			continue
		}

		host := sanitizeHost(strings.TrimSuffix(name, ext))
		if host == "" {
			continue
		}

		iconPath := filepath.Join(s.iconDir, name)
		data, err := os.ReadFile(iconPath)
		if err != nil {
			continue
		}

		encoded := base64.StdEncoding.EncodeToString(data)
		dataURL := "data:image/png;base64," + encoded
		icons[host] = dataURL

		var alias string
		if bare, ok := strings.CutPrefix(host, "www."); ok && bare != "" {
			alias = bare
		} else {
			alias = "www." + host
		}
		if _, exists := icons[alias]; !exists {
			icons[alias] = dataURL
		}
	}

	return icons, nil
}

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

		ctxWasDone := ctx != nil && ctx.Err() != nil
		err := s.fetchAndStoreIcon(ctx, sanitized, trackerURL)
		if err != nil {
			shouldRecordFailure := !errors.Is(err, context.Canceled)
			if ctxWasDone && errors.Is(err, context.DeadlineExceeded) {
				shouldRecordFailure = false
			}
			if shouldRecordFailure {
				s.recordFailure(sanitized)
			}
			if errors.Is(err, ErrIconNotFound) {
				log.Trace().Str("host", sanitized).Err(err).Msg("Tracker icon fetch failed")
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

func (s *Service) iconPath(host string) string {
	filename := safeFilename(host) + ".png"
	return filepath.Join(s.iconDir, filename)
}

func (s *Service) fetchAndStoreIcon(ctx context.Context, host, trackerURL string) error {
	ctx, cancel := ensureContext(ctx)
	defer cancel()

	iconPath := s.iconPath(host)
	attempted := make(map[string]struct{})
	var lastErr error
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
			if isHostNotFound(err) {
				return fmt.Errorf("%w: %w", ErrIconNotFound, err)
			}
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

			for _, fallbackPath := range []string{"/favicon.ico", "/favicon.png", "/apple-touch-icon.png"} {
				fallback := baseURL.ResolveReference(&url.URL{Path: fallbackPath}).String()
				if _, ok := attempted[fallback]; ok {
					continue
				}
				if _, seen := localSeen[fallback]; seen {
					continue
				}
				localSeen[fallback] = struct{}{}
				iconURLs = append(iconURLs, fallback)
			}
		}

		for _, iconURL := range iconURLs {
			attempted[iconURL] = struct{}{}

			data, contentType, err := s.fetchIconBytes(ctx, iconURL)
			if err != nil {
				if lastErr == nil || !errors.Is(err, context.DeadlineExceeded) {
					lastErr = err
				}
				continue
			}

			img, err := decodeImage(data, contentType, iconURL)
			if err != nil {
				lastErr = err
				continue
			}

			resized := resizeToSquare(img, 16)
			if err := s.writePNG(resized, iconPath); err != nil {
				return err
			}

			if s.activityPublisher != nil {
				s.activityPublisher.Publish(activity.Event{Kind: activity.KindTrackerIcons})
			}

			return nil
		}
	}

	if lastErr != nil {
		return fmt.Errorf("%w: %w", ErrIconNotFound, lastErr)
	}
	return ErrIconNotFound
}

func (s *Service) discoverIcons(ctx context.Context, baseURL *url.URL) ([]string, error) {
	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, baseURL.String(), nil)
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

	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, iconURL, nil)
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
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return fmt.Errorf("encode png: %w", err)
	}
	return s.writeFileAtomic(path, buf.Bytes())
}

func (s *Service) writeFileAtomic(path string, data []byte) error {
	tmpFile, err := os.CreateTemp(s.iconDir, "tracker-icon-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmpFile.Name()
	tmpFile.Close()

	if err := os.WriteFile(tmpName, data, 0o600); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

func (s *Service) buildBaseCandidates(host, trackerURL string) []*url.URL {
	host = sanitizeHost(host)
	if host == "" {
		return nil
	}

	allowHTTP := false
	if trackerURL != "" {
		if u, err := url.Parse(trackerURL); err == nil && strings.EqualFold(u.Scheme, "http") {
			allowHTTP = true
		}
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
					if allowHTTP {
						add((&url.URL{Scheme: "http", Host: u.Host}).String())
					}
				}
			}
		}
	}

	add((&url.URL{Scheme: "https", Host: host}).String())
	if allowHTTP {
		add((&url.URL{Scheme: "http", Host: host}).String())
	}

	var urls []*url.URL
	for _, raw := range ordered {
		if u, err := url.Parse(raw); err == nil {
			urls = append(urls, u)
		}
	}

	return urls
}

func isHostNotFound(err error) bool {
	dnsErr, ok := errors.AsType[*net.DNSError](err)
	return ok && dnsErr.IsNotFound
}

func failureCooldown(attempts int) time.Duration {
	d := baseFailureCooldown
	for range max(attempts-1, 0) {
		if d >= maxFailureCooldown {
			break
		}
		d *= 2
	}
	return min(d, maxFailureCooldown)
}

func (s *Service) canAttempt(host string) bool {
	s.failureMu.Lock()
	defer s.failureMu.Unlock()

	if f, ok := s.failures[host]; ok {
		if time.Since(f.LastFailure) < failureCooldown(f.Attempts) {
			return false
		}
	}

	return true
}

func (s *Service) recordFailure(host string) {
	s.failureMu.Lock()
	defer s.failureMu.Unlock()

	f := s.failures[host]
	f.Attempts++
	f.LastFailure = time.Now()
	s.failures[host] = f
	s.saveFailuresLocked()
}

func (s *Service) clearFailure(host string) {
	s.failureMu.Lock()
	defer s.failureMu.Unlock()

	if _, ok := s.failures[host]; !ok {
		return
	}
	delete(s.failures, host)
	s.saveFailuresLocked()
}

func (s *Service) failuresPath() string {
	return filepath.Join(s.iconDir, failuresFileName)
}

func (s *Service) loadFailures() error {
	data, err := os.ReadFile(s.failuresPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}

	var failures map[string]failureState
	if err := json.Unmarshal(data, &failures); err != nil {
		return err
	}
	maps.Copy(s.failures, failures)
	return nil
}

func (s *Service) saveFailuresLocked() {
	data, err := json.Marshal(s.failures)
	if err == nil {
		err = s.writeFileAtomic(s.failuresPath(), data)
	}
	if err != nil {
		log.Warn().Err(err).Msg("Tracker icon fetch failures could not be saved")
	}
}

func sanitizeHost(host string) string {
	host = strings.TrimSpace(host)
	host = strings.Trim(host, "/")
	if host == "" {
		return ""
	}

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

	addWithWWW := func(candidate string) {
		add(candidate)
		if !strings.HasPrefix(candidate, "www.") && strings.Count(candidate, ".") == 1 {
			add("www." + candidate)
		}
	}

	addWithWWW(sanitized)

	current := sanitized
	for {
		next := trimLeadingLabel(current)
		if next == "" || next == current {
			break
		}
		if strings.Count(next, ".") == 0 {
			break
		}
		addWithWWW(next)
		current = next
		if strings.Count(current, ".") == 1 {
			break
		}
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
		return nil, "", errors.New("invalid data URI")
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
		return nil, "", errors.New("unsupported data URI encoding")
	}

	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return nil, "", err
	}

	return decoded, contentType, nil
}

func decodeImage(data []byte, contentType, originalURL string) (image.Image, error) {
	if strings.Contains(strings.ToLower(contentType), "svg") || strings.HasSuffix(strings.ToLower(originalURL), ".svg") {
		return nil, errors.New("svg icons are not supported")
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

	cfg, _, err := image.DecodeConfig(reader)
	if err != nil {
		return nil, err
	}

	if err := validateDimensions(cfg.Width, cfg.Height); err != nil {
		return nil, err
	}

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

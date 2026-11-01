// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package peerclienticons downloads the logos of peer clients from a fixed
// list, checks their hash, and keeps them in the data directory.
package peerclienticons

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
	"golang.org/x/sync/singleflight"
)

const (
	dirName          = "peer-client-icons"
	failuresFileName = "fetch-failures.json"
	maxIconBytes     = 1 << 20 // 1 MiB
	fetchTimeout     = 15 * time.Second

	// The same waits as tracker icons: doubled after each failure, capped.
	baseFailureCooldown = 30 * time.Minute
	maxFailureCooldown  = 24 * time.Hour
)

// errNotFound covers every reason the server has no icon to serve.
var errNotFound = errors.New("peer client icon not found")

type failureState struct {
	Attempts    int       `json:"attempts"`
	LastFailure time.Time `json:"lastFailure"`
}

// Service serves the icons of one list from disk, and downloads a missing one.
type Service struct {
	dir          string
	client       *http.Client
	ua           string
	icons        map[string]Icon
	fetchEnabled atomic.Bool
	group        singleflight.Group

	failureMu sync.Mutex
	failures  map[string]failureState
}

// NewService prepares the icon directory in dataDir. Fetching starts enabled.
func NewService(dataDir, userAgent string, icons []Icon) (*Service, error) {
	dir := filepath.Join(dataDir, dirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create peer client icon directory: %w", err)
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyFromEnvironment

	s := &Service{
		dir: dir,
		client: &http.Client{
			Timeout:   fetchTimeout,
			Transport: transport,
			// The URLs are pinned raw files; a redirect is a failure.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		ua:       userAgent,
		icons:    make(map[string]Icon, len(icons)),
		failures: make(map[string]failureState),
	}
	for _, icon := range icons {
		s.icons[icon.Name] = icon
	}
	s.fetchEnabled.Store(true)

	if data, err := os.ReadFile(s.failuresPath()); err == nil {
		if err := json.Unmarshal(data, &s.failures); err != nil || s.failures == nil {
			s.failures = make(map[string]failureState)
		}
	}
	return s, nil
}

// SetFetchEnabled turns downloads on or off. Files on disk are served either way.
func (s *Service) SetFetchEnabled(enabled bool) {
	s.fetchEnabled.Store(enabled)
}

// GetIcon returns the bytes and content type of the named icon.
func (s *Service) GetIcon(ctx context.Context, name string) ([]byte, string, error) {
	icon, ok := s.icons[name]
	if !ok {
		return nil, "", errNotFound
	}

	data, err := s.readVerified(icon)
	if err != nil {
		if !s.fetchEnabled.Load() {
			return nil, "", errNotFound
		}
		// Detached from the request, so one closed browser tab does not count as a failure.
		select {
		case res := <-s.group.DoChan(name, func() (any, error) { return s.fetch(context.WithoutCancel(ctx), icon) }):
			if res.Err != nil {
				return nil, "", errNotFound
			}
			data = res.Val.([]byte)
		case <-ctx.Done():
			return nil, "", ctx.Err()
		}
	}

	if icon.Fill != "" {
		data = bytes.Replace(data, []byte("<svg"), []byte(`<svg fill="`+icon.Fill+`"`), 1)
	}
	return data, icon.ContentType, nil
}

// readVerified checks the hash on each read, so a file that someone put in
// the directory by hand is never served.
func (s *Service) readVerified(icon Icon) ([]byte, error) {
	data, err := os.ReadFile(s.path(icon))
	if err != nil {
		return nil, err
	}
	if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != icon.SHA256 {
		return nil, errNotFound
	}
	return data, nil
}

func (s *Service) fetch(ctx context.Context, icon Icon) ([]byte, error) {
	if data, err := s.readVerified(icon); err == nil {
		return data, nil
	}
	if !s.canAttempt(icon.Name) {
		return nil, errNotFound
	}

	data, err := s.download(ctx, icon)
	if err != nil {
		log.Debug().Err(err).Str("icon", icon.Name).Msg("Peer client icon download failed")
		s.recordFailure(icon.Name)
		return nil, err
	}
	s.clearFailure(icon.Name)
	log.Debug().Str("icon", icon.Name).Msg("Peer client icon downloaded")
	return data, nil
}

func (s *Service) download(ctx context.Context, icon Icon) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, icon.URL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", s.ua)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxIconBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxIconBytes {
		return nil, errors.New("icon larger than 1 MiB")
	}
	if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != icon.SHA256 {
		return nil, errors.New("hash mismatch")
	}
	if err := s.writeFileAtomic(s.path(icon), data); err != nil {
		return nil, err
	}
	return data, nil
}

func (s *Service) path(icon Icon) string {
	ext := ".png"
	if icon.ContentType == svg {
		ext = ".svg"
	}
	return filepath.Join(s.dir, icon.Name+"-"+icon.SHA256+ext)
}

func (s *Service) failuresPath() string {
	return filepath.Join(s.dir, failuresFileName)
}

func (s *Service) writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(s.dir, "peer-client-icon-*.tmp")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
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

func (s *Service) canAttempt(name string) bool {
	s.failureMu.Lock()
	defer s.failureMu.Unlock()
	f, ok := s.failures[name]
	return !ok || time.Since(f.LastFailure) >= failureCooldown(f.Attempts)
}

func (s *Service) recordFailure(name string) {
	s.failureMu.Lock()
	defer s.failureMu.Unlock()
	f := s.failures[name]
	f.Attempts++
	f.LastFailure = time.Now()
	s.failures[name] = f
	s.saveFailuresLocked()
}

func (s *Service) clearFailure(name string) {
	s.failureMu.Lock()
	defer s.failureMu.Unlock()
	if _, ok := s.failures[name]; !ok {
		return
	}
	delete(s.failures, name)
	s.saveFailuresLocked()
}

func (s *Service) saveFailuresLocked() {
	data, err := json.Marshal(s.failures)
	if err == nil {
		err = s.writeFileAtomic(s.failuresPath(), data)
	}
	if err != nil {
		log.Warn().Err(err).Msg("Peer client icon fetch failures could not be saved")
	}
}

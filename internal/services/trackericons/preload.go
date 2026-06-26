// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package trackericons

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var preloadFilenames = []string{
	"preload.json",
	"preload.js",
	"tracker-icons.json",
	"tracker-icons.js",
	"tracker-icons.txt",
}

func (s *Service) preloadIconsFromDisk() error {
	mapping, source, err := readPreloadFile(s.iconDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}

	var joinedErr error

	for rawHost, rawData := range mapping {
		host := sanitizeHost(rawHost)
		if host == "" {
			joinedErr = errors.Join(joinedErr, fmt.Errorf("tracker icon preload: invalid host %q", rawHost))
			continue
		}

		iconPath := s.iconPath(host)
		if _, err := os.Stat(iconPath); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			joinedErr = errors.Join(joinedErr, fmt.Errorf("tracker icon preload: stat %s: %w", iconPath, err))
			continue
		}

		payload, contentType, err := parseDataURL(strings.TrimSpace(rawData))
		if err != nil {
			joinedErr = errors.Join(joinedErr, fmt.Errorf("tracker icon preload: %s: %w", host, err))
			continue
		}

		img, err := decodeImage(payload, contentType, "preload:"+host)
		if err != nil {
			joinedErr = errors.Join(joinedErr, fmt.Errorf("tracker icon preload: %s: %w", host, err))
			continue
		}

		resized := resizeToSquare(img, 16)
		if err := s.writePNG(resized, iconPath); err != nil {
			joinedErr = errors.Join(joinedErr, fmt.Errorf("tracker icon preload: write %s: %w", host, err))
			continue
		}

		if alias := strings.TrimPrefix(host, "www."); alias != host && alias != "" {
			aliasPath := s.iconPath(alias)
			if _, err := os.Stat(aliasPath); errors.Is(err, os.ErrNotExist) {
				if err := s.writePNG(resized, aliasPath); err != nil {
					joinedErr = errors.Join(joinedErr, fmt.Errorf("tracker icon preload: write alias %s: %w", alias, err))
				}
			}
		}
	}

	if joinedErr != nil {
		return fmt.Errorf("preload tracker icons from %s: %w", source, joinedErr)
	}

	return nil
}

func readPreloadFile(iconDir string) (map[string]string, string, error) {
	for _, name := range preloadFilenames {
		full := filepath.Join(iconDir, name)
		data, err := os.ReadFile(full)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return nil, "", fmt.Errorf("read %s: %w", full, err)
		}

		mapping, err := parseIconMapping(data)
		if err != nil {
			return nil, "", fmt.Errorf("parse %s: %w", full, err)
		}

		if len(mapping) == 0 {
			continue
		}

		return mapping, full, nil
	}

	return nil, "", fs.ErrNotExist
}

func parseIconMapping(raw []byte) (map[string]string, error) {
	content := strings.TrimSpace(string(raw))
	if content == "" {
		return nil, nil
	}

	if idx := strings.Index(content, "{"); idx >= 0 {
		if last := strings.LastIndex(content, "}"); last > idx {
			content = content[idx : last+1]
		}
	}

	content = strings.TrimSpace(content)
	if strings.HasSuffix(content, ";") {
		content = strings.TrimSpace(content[:len(content)-1])
	}

	content = trailingCommaRE.ReplaceAllString(content, "}")

	var mapping map[string]string
	if err := json.Unmarshal([]byte(content), &mapping); err != nil {
		return nil, err
	}

	cleaned := make(map[string]string, len(mapping))
	for k, v := range mapping {
		cleaned[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}

	return cleaned, nil
}

var trailingCommaRE = regexp.MustCompile(`,\s*}`)

func parseDataURL(raw string) ([]byte, string, error) {
	if raw == "" {
		return nil, "", fmt.Errorf("empty data URL")
	}

	if !strings.HasPrefix(strings.ToLower(raw), "data:") {
		return nil, "", fmt.Errorf("data URL must start with 'data:'")
	}

	idx := strings.Index(raw, ",")
	if idx < 0 {
		return nil, "", fmt.Errorf("invalid data URL")
	}

	meta := raw[len("data:"):idx]
	payload := raw[idx+1:]

	mediaType := "application/octet-stream"
	base64Encoded := false

	if meta != "" {
		parts := strings.Split(meta, ";")
		if t := strings.TrimSpace(parts[0]); t != "" {
			mediaType = strings.ToLower(t)
		}

		for _, part := range parts[1:] {
			if strings.EqualFold(strings.TrimSpace(part), "base64") {
				base64Encoded = true
				break
			}
		}
	}

	payload = strings.TrimSpace(payload)
	if payload == "" {
		return nil, "", fmt.Errorf("empty data payload")
	}

	payload = strings.Map(func(r rune) rune {
		switch r {
		case '\n', '\r', '\t', ' ':
			return -1
		default:
			return r
		}
	}, payload)

	var data []byte
	var err error

	if base64Encoded {
		data, err = base64.StdEncoding.DecodeString(payload)
		if err != nil {
			return nil, "", fmt.Errorf("decode base64: %w", err)
		}
	} else {
		decoded, err := url.PathUnescape(payload)
		if err != nil {
			return nil, "", fmt.Errorf("decode payload: %w", err)
		}
		data = []byte(decoded)
	}

	return data, mediaType, nil
}

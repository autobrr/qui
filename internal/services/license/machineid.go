// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package license

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/keygen-sh/machineid"
	"github.com/rs/zerolog/log"
)

func GetDeviceID(appID string, userID string, configDir string) (string, error) {
	fingerprintPath := getFingerprintPath(userID, configDir)
	if content, err := os.ReadFile(fingerprintPath); err == nil {
		existing := strings.TrimSpace(string(content))
		if existing != "" {
			log.Trace().Str("path", fingerprintPath).Msg("using existing fingerprint")
			return existing, nil
		}
	}

	baseID, err := machineid.ProtectedID(appID)
	if err != nil {
		log.Warn().Err(err).Msg("failed to get machine ID, using fallback")
		baseID = generateFallbackMachineID()
	}

	combined := fmt.Sprintf("%s-%s-%s", appID, baseID, userID)
	hash := sha256.Sum256([]byte(combined))
	fingerprint := fmt.Sprintf("%x", hash)

	return persistFingerprint(fingerprint, userID, configDir)
}

func generateFallbackMachineID() string {
	hostInfo := fmt.Sprintf("%s-%s", runtime.GOOS, runtime.GOARCH)

	if hostname, err := os.Hostname(); err == nil {
		hostInfo = fmt.Sprintf("%s-%s", hostInfo, hostname)
	}

	hash := sha256.Sum256([]byte(hostInfo))
	return fmt.Sprintf("%x", hash)[:32]
}

func persistFingerprint(fingerprint, userID string, configDir string) (string, error) {
	fingerprintPath := getFingerprintPath(userID, configDir)

	if err := os.MkdirAll(filepath.Dir(fingerprintPath), 0755); err != nil {
		log.Warn().Err(err).Str("path", fingerprintPath).Msg("failed to create fingerprint directory")
		return fingerprint, nil
	}

	if err := os.WriteFile(fingerprintPath, []byte(fingerprint), 0644); err != nil {
		log.Warn().Err(err).Str("path", fingerprintPath).Msg("failed to persist fingerprint")
		return fingerprint, nil
	}

	log.Trace().Str("path", fingerprintPath).Msg("persisted new fingerprint")

	return fingerprint, nil
}

func getFingerprintPath(userID string, configDir string) string {
	userHash := sha256.Sum256([]byte(userID))
	filename := fmt.Sprintf(".device-id-%x", userHash)[:20]

	return filepath.Join(configDir, filename)
}

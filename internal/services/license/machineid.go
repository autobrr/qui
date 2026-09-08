// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package license

import (
	"crypto/sha256"
	"encoding/hex"
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
			restrictFingerprintMode(fingerprintPath)
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
	fingerprint := hex.EncodeToString(hash[:])

	return persistFingerprint(fingerprint, userID, configDir)
}

func generateFallbackMachineID() string {
	hostInfo := fmt.Sprintf("%s-%s", runtime.GOOS, runtime.GOARCH)

	if hostname, err := os.Hostname(); err == nil {
		hostInfo = fmt.Sprintf("%s-%s", hostInfo, hostname)
	}

	hash := sha256.Sum256([]byte(hostInfo))
	return hex.EncodeToString(hash[:])[:32]
}

func persistFingerprint(fingerprint, userID string, configDir string) (string, error) {
	fingerprintPath := getFingerprintPath(userID, configDir)

	if err := os.MkdirAll(filepath.Dir(fingerprintPath), 0755); err != nil {
		log.Warn().Err(err).Str("path", fingerprintPath).Msg("failed to create fingerprint directory")
		return fingerprint, nil
	}

	if err := os.WriteFile(fingerprintPath, []byte(fingerprint), 0o600); err != nil {
		log.Warn().Err(err).Str("path", fingerprintPath).Msg("failed to persist fingerprint")
		return fingerprint, nil
	}
	restrictFingerprintMode(fingerprintPath)

	log.Trace().Str("path", fingerprintPath).Msg("persisted new fingerprint")

	return fingerprint, nil
}

// restrictFingerprintMode narrows an existing fingerprint file to 0600. Files
// written by older versions are 0644, and WriteFile only applies its mode when
// it creates the file, so neither path tightens one on its own.
func restrictFingerprintMode(path string) {
	if err := os.Chmod(path, 0o600); err != nil {
		log.Warn().Err(err).Str("path", path).Msg("failed to restrict fingerprint permissions")
	}
}

func getFingerprintPath(userID string, configDir string) string {
	userHash := sha256.Sum256([]byte(userID))
	filename := fmt.Sprintf(".device-id-%x", userHash)[:20]

	return filepath.Join(configDir, filename)
}

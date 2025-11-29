// Copyright (c) 2025, s0up and the autobrr contributors.
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

// GetDeviceID retrieves or generates a device fingerprint.
// Priority: 1. Existing file, 2. Generate new
// For database fallback support, use GetDeviceIDWithFallback instead.
func GetDeviceID(appID string, userID string, configDir string) (string, error) {
	return GetDeviceIDWithFallback(appID, userID, configDir, "")
}

// GetDeviceIDWithFallback retrieves or generates a device fingerprint with database fallback support.
// Priority: 1. Existing file, 2. Database fallback (if provided), 3. Generate new
// The dbFingerprint parameter allows recovering from lost fingerprint files by using
// a previously stored fingerprint from the database.
func GetDeviceIDWithFallback(appID string, userID string, configDir string, dbFingerprint string) (string, error) {
	fingerprintPath := getFingerprintPath(userID, configDir)

	// Priority 1: Check for existing fingerprint file
	if content, err := os.ReadFile(fingerprintPath); err == nil {
		existing := strings.TrimSpace(string(content))
		if existing != "" {
			log.Trace().Str("path", fingerprintPath).Msg("using existing fingerprint from file")
			return existing, nil
		}
	}

	// Priority 2: Use database fingerprint if available (recovery scenario)
	if dbFingerprint != "" {
		log.Info().Str("path", fingerprintPath).Msg("fingerprint file not found, recovering from database")
		// Re-persist the fingerprint to file for future use
		if _, err := persistFingerprint(dbFingerprint, userID, configDir); err != nil {
			log.Warn().Err(err).Msg("failed to re-persist fingerprint from database, continuing anyway")
		}
		return dbFingerprint, nil
	}

	// Priority 3: Generate new fingerprint
	log.Info().Msg("generating new device fingerprint")
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

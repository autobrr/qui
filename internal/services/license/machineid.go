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

func GetDeviceID(appID string, userID string) (string, error) {
	fingerprintPath := getFingerprintPath(userID)
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

	return persistFingerprint(fingerprint, userID)
}

func isRunningInContainer() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}

	if os.Getenv("KUBERNETES_SERVICE_HOST") != "" {
		return true
	}

	if strings.Contains(os.Getenv("container"), "podman") {
		return true
	}

	return false
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func generateFallbackMachineID() string {
	hostInfo := fmt.Sprintf("%s-%s", runtime.GOOS, runtime.GOARCH)

	if hostname, err := os.Hostname(); err == nil {
		hostInfo = fmt.Sprintf("%s-%s", hostInfo, hostname)
	}

	hash := sha256.Sum256([]byte(hostInfo))
	return fmt.Sprintf("%x", hash)[:32]
}

func persistFingerprint(fingerprint, userID string) (string, error) {
	fingerprintPath := getFingerprintPath(userID)

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

func getFingerprintPath(userID string) string {
	var configDir string

	if isRunningInContainer() {
		containerPaths := []string{
			"/config",
			"/var/lib/qui",
			"/tmp",
		}
		for _, path := range containerPaths {
			if dirExists(path) {
				configDir = path
				break
			}
		}
	} else {
		if homeDir, err := os.UserHomeDir(); err == nil {
			switch runtime.GOOS {
			case "windows":
				configDir = filepath.Join(homeDir, "AppData", "Roaming", "qui")
			case "darwin":
				configDir = filepath.Join(homeDir, "Library", "Application Support", "qui")
			default: // linux, bsd, etc
				if xdgConfig := os.Getenv("XDG_CONFIG_HOME"); xdgConfig != "" {
					configDir = filepath.Join(xdgConfig, "qui")
				} else {
					configDir = filepath.Join(homeDir, ".config", "qui")
				}
			}
		}
	}

	// fallback to tmp
	if configDir == "" {
		configDir = filepath.Join(os.TempDir(), "qui")
	}

	userHash := sha256.Sum256([]byte(userID))
	filename := fmt.Sprintf(".device-id-%x", userHash)[:20]

	return filepath.Join(configDir, filename)
}

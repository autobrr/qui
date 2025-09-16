package license

import (
	"crypto/rand"
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

func GetDeviceID(appID string) (string, error) {
	id, err := machineid.ProtectedID(appID)
	if err != nil {
		return "", err
	}

	if isRunningInContainer() {
		log.Trace().Msg("get device id, running in container")
		if persistentID := getPersistentContainerID(); persistentID != "" {
			combined := fmt.Sprintf("%s-%s", appID, persistentID)
			hash := sha256.Sum256([]byte(combined))
			return fmt.Sprintf("%x", hash), nil
		}
	}

	return id, nil
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

func getPersistentContainerID() string {
	persistentPaths := []string{
		"/config/.qui-device-id",
		"/var/lib/qui/.qui-device-id",
	}

	for _, path := range persistentPaths {
		if content, err := os.ReadFile(path); err == nil {
			return strings.TrimSpace(string(content))
		}
	}

	for _, path := range persistentPaths {
		if dir := filepath.Dir(path); dirExists(dir) {
			newID := generateRandomID()
			if err := os.WriteFile(path, []byte(newID), 0644); err == nil {
				return newID
			}
		}
	}

	return ""
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func generateRandomID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to a deterministic but unique ID based on current state
		hash := sha256.Sum256([]byte(fmt.Sprintf("%d-%s", os.Getpid(), runtime.GOOS)))
		return fmt.Sprintf("%x", hash)[:32]
	}
	return hex.EncodeToString(bytes)
}

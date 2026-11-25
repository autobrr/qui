// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package problemreport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/domain"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/redact"
)

type Secrets struct {
	Values    []string
	KeyHashes map[string]struct{}
}

type StoreSecrets struct {
	Instances     *models.InstanceStore
	Indexers      *models.TorznabIndexerStore
	Arr           *models.ArrInstanceStore
	CrossSeed     *models.CrossSeedStore
	APIKeys       *models.APIKeyStore
	ClientAPIKeys *models.ClientAPIKeyStore
	Notifications *models.NotificationTargetStore
	Licenses      *database.LicenseRepo
	Config        func() []string
}

func ConfigSecrets(cfg *domain.Config) []string {
	secrets := []string{cfg.SessionSecret, cfg.DatabasePassword, cfg.OIDCClientSecret}
	if dsn := strings.TrimSpace(cfg.DatabaseDSN); dsn != "" {
		if parsed, err := pgconn.ParseConfig(dsn); err == nil {
			secrets = append(secrets, parsed.Password)
		}
	}
	return secrets
}

func (s StoreSecrets) ReportSecrets(ctx context.Context) (Secrets, error) {
	c := &secretCollector{secrets: Secrets{KeyHashes: map[string]struct{}{}}}
	if s.Config != nil {
		c.add(s.Config()...)
	}
	for _, read := range []func(context.Context, *secretCollector) error{
		s.instanceSecrets, s.indexerSecrets, s.arrSecrets, s.crossSeedSecrets, s.keyHashes, s.notificationSecrets, s.licenseSecrets,
	} {
		if err := read(ctx, c); err != nil {
			return Secrets{}, err
		}
	}
	return c.secrets, nil
}

type secretCollector struct {
	secrets Secrets
}

func (c *secretCollector) add(values ...string) {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			c.secrets.Values = append(c.secrets.Values, value)
		}
	}
}

func (c *secretCollector) decrypted(value string, err error) {
	if err == nil {
		c.add(value)
	}
}

func (s StoreSecrets) instanceSecrets(ctx context.Context, c *secretCollector) error {
	if s.Instances == nil {
		return nil
	}
	instances, err := s.Instances.List(ctx)
	if err != nil {
		return fmt.Errorf("list instances: %w", err)
	}
	for _, instance := range instances {
		c.decrypted(s.Instances.GetDecryptedPassword(instance))
		if instance.APIKeyEncrypted != "" {
			c.decrypted(s.Instances.GetDecryptedAPIKey(instance))
		}
		if password, err := s.Instances.GetDecryptedBasicPassword(instance); err == nil && password != nil {
			c.add(*password)
		}
		c.add(instanceAddress(instance.Host)...)
		c.add(instance.Username)
		if instance.BasicUsername != nil {
			c.add(*instance.BasicUsername)
		}
		c.decrypted(s.Instances.GetDecryptedSSHKey(instance))
		c.add(instanceAddress(instance.SSHHost)...)
		c.add(instance.SSHUsername)
		c.add(instanceAddress(instance.PublicPeerAddress)...)
	}
	return nil
}

func (s StoreSecrets) indexerSecrets(ctx context.Context, c *secretCollector) error {
	if s.Indexers == nil {
		return nil
	}
	indexers, err := s.Indexers.List(ctx)
	if err != nil {
		return fmt.Errorf("list indexers: %w", err)
	}
	for _, indexer := range indexers {
		c.decrypted(s.Indexers.GetDecryptedAPIKey(indexer))
		c.decrypted(s.Indexers.GetDecryptedBasicPassword(indexer))
	}
	return nil
}

func (s StoreSecrets) arrSecrets(ctx context.Context, c *secretCollector) error {
	if s.Arr == nil {
		return nil
	}
	instances, err := s.Arr.List(ctx)
	if err != nil {
		return fmt.Errorf("list arr instances: %w", err)
	}
	for _, instance := range instances {
		c.decrypted(s.Arr.GetDecryptedAPIKey(instance))
		c.decrypted(s.Arr.GetDecryptedBasicPassword(instance))
	}
	return nil
}

func (s StoreSecrets) crossSeedSecrets(ctx context.Context, c *secretCollector) error {
	if s.CrossSeed == nil {
		return nil
	}
	keys, err := s.CrossSeed.GetStoredGazelleAPIKeys(ctx)
	if err != nil {
		return fmt.Errorf("read RED/OPS keys: %w", err)
	}
	for _, key := range keys.Keys {
		c.add(key)
	}
	apiKey, pin, err := s.CrossSeed.GetDecryptedSeasonPackTVDBCredentials(ctx)
	if err != nil && !errors.Is(err, models.ErrUndecryptableCredential) {
		return fmt.Errorf("read TVDB credentials: %w", err)
	}
	c.add(apiKey, pin)
	return nil
}

func (s StoreSecrets) keyHashes(ctx context.Context, c *secretCollector) error {
	if s.APIKeys != nil {
		keys, err := s.APIKeys.List(ctx)
		if err != nil {
			return fmt.Errorf("list API keys: %w", err)
		}
		for _, key := range keys {
			c.secrets.KeyHashes[key.KeyHash] = struct{}{}
		}
	}
	if s.ClientAPIKeys != nil {
		keys, err := s.ClientAPIKeys.GetAll(ctx)
		if err != nil {
			return fmt.Errorf("list client API keys: %w", err)
		}
		for _, key := range keys {
			c.secrets.KeyHashes[key.KeyHash] = struct{}{}
		}
	}
	return nil
}

func (s StoreSecrets) notificationSecrets(ctx context.Context, c *secretCollector) error {
	if s.Notifications == nil {
		return nil
	}
	targets, err := s.Notifications.List(ctx)
	if err != nil {
		return fmt.Errorf("list notification targets: %w", err)
	}
	for _, target := range targets {
		c.add(notificationURLSecrets(target.URL)...)
	}
	return nil
}

func (s StoreSecrets) licenseSecrets(ctx context.Context, c *secretCollector) error {
	if s.Licenses == nil {
		return nil
	}
	licenses, err := s.Licenses.GetAllLicenses(ctx)
	if err != nil {
		return fmt.Errorf("list licenses: %w", err)
	}
	for _, license := range licenses {
		c.add(license.LicenseKey)
	}
	return nil
}

var hostCredentialSchemes = map[string]bool{
	"notifiarr": true, "notifiarrapi": true, "pushover": true, "pushbullet": true, "ifttt": true, "wecom": true,
}

func notificationURLSecrets(raw string) []string {
	secrets := []string{raw}
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return secrets
	}
	if parsed.User != nil {
		password, _ := parsed.User.Password()
		secrets = append(secrets, parsed.User.Username(), password)
	}
	if hostCredentialSchemes[parsed.Scheme] {
		secrets = append(secrets, parsed.Host)
	}
	for segment := range strings.SplitSeq(parsed.Path, "/") {
		if !urlStructureWords[strings.ToLower(segment)] {
			secrets = append(secrets, segment)
		}
	}
	return secrets
}

var urlStructureWords = map[string]bool{
	"webhook": true, "webhooks": true, "message": true, "messages": true, "services": true, "notify": true,
}

func instanceAddress(host string) []string {
	host = strings.TrimSpace(host)
	if host == "" {
		return nil
	}
	parsed, err := url.Parse(host)
	if err != nil || parsed.Host == "" {
		parsed, err = url.Parse("http://" + host)
		if err != nil {
			return []string{host}
		}
	}
	name := parsed.Hostname()
	identifies := net.ParseIP(name) != nil || strings.Contains(name, ".")
	var forms []string
	if identifies || parsed.Port() != "" {
		forms = append(forms, parsed.Host)
	}
	if identifies {
		forms = append(forms, name)
	}
	bare := parsed.Port() == "" && parsed.User == nil && strings.Trim(parsed.Path, "/") == "" && parsed.RawQuery == ""
	if strings.Contains(host, "://") && (identifies || !bare) {
		forms = append(forms, host, strings.TrimSuffix(host, "/"))
	}
	return forms
}

var apiKeyRegex = regexp.MustCompile(`\b[0-9a-f]{64}\b`)

var licensePathRegex = regexp.MustCompile(`/api/license/[^/?#\s"'<>\\]+`)

var licenseRoutes = map[string]bool{"licensed": true, "licenses": true, "activate": true, "validate": true, "refresh": true}

type cleaner struct {
	values    *strings.Replacer
	keyHashes map[string]struct{}
}

func newCleaner(secrets Secrets) cleaner {
	return cleaner{values: redact.SecretsReplacer(secrets.Values), keyHashes: secrets.KeyHashes}
}

func (c cleaner) clean(text string) string {
	if text == "" {
		return text
	}
	if c.values != nil {
		text = c.values.Replace(text)
	}
	if len(c.keyHashes) > 0 {
		text = apiKeyRegex.ReplaceAllStringFunc(text, func(match string) string {
			sum := sha256.Sum256([]byte(match))
			if _, ok := c.keyHashes[hex.EncodeToString(sum[:])]; ok {
				return "REDACTED"
			}
			return match
		})
	}
	text = licensePathRegex.ReplaceAllStringFunc(text, func(match string) string {
		if segment := strings.TrimPrefix(match, "/api/license/"); licenseRoutes[segment] {
			return match
		}
		return "/api/license/REDACTED"
	})
	return redact.Text(text)
}

func (c cleaner) cleanAll(lines []string) []string {
	cleaned := make([]string, len(lines))
	for i, line := range lines {
		cleaned[i] = c.clean(line)
	}
	return cleaned
}

func (c cleaner) cleanValue(value any) any {
	switch v := value.(type) {
	case string:
		return c.clean(v)
	case []any:
		for i := range v {
			v[i] = c.cleanValue(v[i])
		}
		return v
	case map[string]any:
		cleaned := make(map[string]any, len(v))
		for key, item := range v {
			cleaned[c.clean(key)] = c.cleanValue(item)
		}
		return cleaned
	default:
		return value
	}
}

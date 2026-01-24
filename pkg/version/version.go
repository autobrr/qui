// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package version

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	goversion "github.com/hashicorp/go-version"
)

// Release models the parts of a GitHub release we care about.
type Release struct {
	ID              int64     `json:"id,omitempty"`
	NodeID          string    `json:"node_id,omitempty"`
	URL             string    `json:"url,omitempty"`
	HTMLURL         string    `json:"html_url,omitempty"`
	TagName         string    `json:"tag_name,omitempty"`
	TargetCommitish string    `json:"target_commitish,omitempty"`
	Name            *string   `json:"name,omitempty"`
	Body            *string   `json:"body,omitempty"`
	Draft           bool      `json:"draft,omitempty"`
	Prerelease      bool      `json:"prerelease,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	PublishedAt     time.Time `json:"published_at"`
	Author          Author    `json:"author"`
	Assets          []Asset   `json:"assets"`
}

// Author represents the author of a release.
type Author struct {
	Login      string `json:"login"`
	ID         int64  `json:"id"`
	NodeID     string `json:"node_id"`
	AvatarURL  string `json:"avatar_url"`
	GravatarID string `json:"gravatar_id"`
	URL        string `json:"url"`
	HTMLURL    string `json:"html_url"`
	Type       string `json:"type"`
}

// Asset models a downloadable artifact for a release.
type Asset struct {
	URL                string    `json:"url"`
	ID                 int64     `json:"id"`
	NodeID             string    `json:"node_id"`
	Name               string    `json:"name"`
	Label              string    `json:"label"`
	Uploader           Author    `json:"uploader"`
	ContentType        string    `json:"content_type"`
	State              string    `json:"state"`
	Size               int64     `json:"size"`
	DownloadCount      int64     `json:"download_count"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
	BrowserDownloadURL string    `json:"browser_download_url"`
}

// Checker talks to api.autobrr.com to determine whether a newer release is available.
type Checker struct {
	Owner     string
	Repo      string
	UserAgent string

	httpClient *http.Client
}

// NewChecker returns a configured Checker for the provided repository.
func NewChecker(owner, repo, userAgent string) *Checker {
	return &Checker{
		Owner:     owner,
		Repo:      repo,
		UserAgent: userAgent,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *Checker) get(ctx context.Context) (*Release, error) {
	url := fmt.Sprintf("https://api.autobrr.com/repos/%s/%s/releases/latest", c.Owner, c.Repo)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/vnd.github.v3+json")

	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("error getting releases for %s: %s", c.Repo, resp.Status)
	}

	var release Release
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&release); err != nil {
		return nil, err
	}

	return &release, nil
}

// CheckNewVersion returns whether a newer release is available and the release metadata.
func (c *Checker) CheckNewVersion(ctx context.Context, version string) (bool, *Release, error) {
	if isDevelop(version) {
		return false, nil, nil
	}

	release, err := c.get(ctx)
	if err != nil {
		return false, nil, err
	}

	newAvailable, _, err := c.compareVersions(version, release)
	if err != nil {
		return false, nil, err
	}

	if !newAvailable {
		return false, nil, nil
	}

	return true, release, nil
}

func (c *Checker) compareVersions(version string, release *Release) (bool, string, error) {
	currentVersion, err := goversion.NewVersion(version)
	if err != nil {
		return false, "", fmt.Errorf("error parsing current version: %w", err)
	}

	releaseVersion, err := goversion.NewVersion(release.TagName)
	if err != nil {
		return false, "", fmt.Errorf("error parsing release version: %w", err)
	}

	if len(currentVersion.Prerelease()) == 0 && len(releaseVersion.Prerelease()) > 0 {
		return false, "", nil
	}

	if releaseVersion.GreaterThan(currentVersion) {
		return true, releaseVersion.String(), nil
	}

	return false, "", nil
}

func isDevelop(version string) bool {
	if strings.HasPrefix(version, "pr-") || strings.HasSuffix(version, "-dev") || strings.HasSuffix(version, "-develop") {
		return true
	}

	tags := []string{"dev", "develop", "main", "latest", ""}
	return slices.Contains(tags, version)
}

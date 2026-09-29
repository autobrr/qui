// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package update

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"io"

	"github.com/creativeprojects/go-selfupdate"
)

//go:embed release_signing_cert.pem
var releaseSigningCertificate []byte

func (u *Updater) newSelfUpdater(backup string) (*selfupdate.Updater, *archiveSource, error) {
	validator, err := newReleaseValidator(u.certificate)
	if err != nil {
		return nil, nil, err
	}

	source := u.source
	if source == nil {
		// The go-selfupdate default.
		if source, err = selfupdate.NewGitHubSource(selfupdate.GitHubConfig{}); err != nil {
			return nil, nil, fmt.Errorf("could not create GitHub source: %w", err)
		}
	}
	archive := &archiveSource{Source: source}
	updater, err := selfupdate.NewUpdater(selfupdate.Config{
		Source:      archive,
		Validator:   validator,
		OldSavePath: backup,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("could not create updater: %w", err)
	}

	return updater, archive, nil
}

// archiveSource keeps the release archive that UpdateTo downloads. After
// UpdateTo validated it, the sibling binary comes from the same bytes, so the
// release is downloaded and verified once.
type archiveSource struct {
	selfupdate.Source
	archive []byte
}

func (s *archiveSource) DownloadReleaseAsset(ctx context.Context, rel *selfupdate.Release, assetID int64) (io.ReadCloser, error) {
	body, err := s.Source.DownloadReleaseAsset(ctx, rel, assetID)
	if err != nil || assetID != rel.AssetID {
		return body, err //nolint:wrapcheck // go-selfupdate wraps it
	}
	defer body.Close()
	s.archive, err = io.ReadAll(body)
	return io.NopCloser(bytes.NewReader(s.archive)), err //nolint:wrapcheck // go-selfupdate wraps it
}

func newReleaseValidator(certificate []byte) (_ selfupdate.Validator, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("could not initialize release validator: %v", r)
		}
	}()

	return selfupdate.NewChecksumWithECDSAValidator(releaseChecksumsAsset, certificate), nil
}

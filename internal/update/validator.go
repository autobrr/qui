// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package update

import (
	_ "embed"
	"fmt"

	"github.com/creativeprojects/go-selfupdate"
)

//go:embed release_signing_cert.pem
var releaseSigningCertificate []byte

func newSelfUpdater() (*selfupdate.Updater, error) {
	validator, err := newReleaseValidator(releaseSigningCertificate)
	if err != nil {
		return nil, err
	}

	updater, err := selfupdate.NewUpdater(selfupdate.Config{
		Validator: validator,
	})
	if err != nil {
		return nil, fmt.Errorf("could not create updater: %w", err)
	}

	return updater, nil
}

func newReleaseValidator(certificate []byte) (_ selfupdate.Validator, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("could not initialize release validator: %v", r)
		}
	}()

	return selfupdate.NewChecksumWithECDSAValidator(releaseChecksumsAsset, certificate), nil
}

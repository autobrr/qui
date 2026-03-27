// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package update

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNewSelfUpdater(t *testing.T) {
	updater, err := newSelfUpdater()
	require.NoError(t, err)
	require.NotNil(t, updater)
}

func TestReleaseValidatorValidatesSignedChecksums(t *testing.T) {
	certificate, privateKey := generateECDSACertificate(t)
	validator, err := newReleaseValidator(certificate)
	require.NoError(t, err)

	assetName := "qui_1.2.3_linux_x86_64.tar.gz"
	asset := []byte("release payload")
	checksums := []byte(fmt.Sprintf("%x  %s\n", sha256.Sum256(asset), assetName))
	signature := signECDSA(t, privateKey, checksums)

	require.Equal(t, releaseChecksumsAsset, validator.GetValidationAssetName(assetName))
	require.Equal(t, releaseChecksumsAsset+".sig", validator.GetValidationAssetName(releaseChecksumsAsset))
	require.NoError(t, validator.Validate(assetName, asset, checksums))
	require.NoError(t, validator.Validate(releaseChecksumsAsset, checksums, signature))
}

func TestReleaseValidatorRejectsTamperedSignedChecksums(t *testing.T) {
	certificate, privateKey := generateECDSACertificate(t)
	validator, err := newReleaseValidator(certificate)
	require.NoError(t, err)

	checksums := []byte("deadbeef  qui_1.2.3_linux_x86_64.tar.gz\n")
	signature := signECDSA(t, privateKey, checksums)
	tamperedChecksums := []byte("feedface  qui_1.2.3_linux_x86_64.tar.gz\n")

	err = validator.Validate(releaseChecksumsAsset, tamperedChecksums, signature)
	require.Error(t, err)
}

func TestReleaseValidatorRejectsInvalidPublicKey(t *testing.T) {
	_, err := newReleaseValidator([]byte("not a public key"))
	require.Error(t, err)
}

func generateECDSACertificate(t *testing.T) ([]byte, *ecdsa.PrivateKey) {
	t.Helper()

	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageCodeSigning},
	}

	certificateDER, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	require.NoError(t, err)

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificateDER}), privateKey
}

func signECDSA(t *testing.T, privateKey *ecdsa.PrivateKey, data []byte) []byte {
	t.Helper()

	digest := sha256.Sum256(data)
	signature, err := ecdsa.SignASN1(rand.Reader, privateKey, digest[:])
	require.NoError(t, err)

	return signature
}

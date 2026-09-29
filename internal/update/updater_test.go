// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/creativeprojects/go-selfupdate"
	"github.com/stretchr/testify/require"
)

func TestNewSelfUpdater(t *testing.T) {
	updater, err := NewUpdater(Config{Repository: "autobrr/qui", Version: "1.30.0"}).newSelfUpdater("")
	require.NoError(t, err)
	require.NotNil(t, updater)
}

func TestReleaseValidatorValidatesSignedChecksums(t *testing.T) {
	certificate, privateKey := generateECDSACertificate(t)
	validator, err := newReleaseValidator(certificate)
	require.NoError(t, err)

	assetName := "qui_1.2.3_linux_x86_64.tar.gz"
	asset := []byte("release payload")
	checksums := fmt.Appendf(nil, "%x  %s\n", sha256.Sum256(asset), assetName)
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

type fakeAsset struct {
	id   int64
	name string
	data []byte
}

func (a fakeAsset) GetID() int64                  { return a.id }
func (a fakeAsset) GetName() string               { return a.name }
func (a fakeAsset) GetSize() int                  { return len(a.data) }
func (a fakeAsset) GetBrowserDownloadURL() string { return "http://127.0.0.1/" + a.name }

type fakeRelease struct {
	id     int64
	tag    string
	assets []fakeAsset
}

func (r fakeRelease) GetID() int64              { return r.id }
func (r fakeRelease) GetTagName() string        { return r.tag }
func (r fakeRelease) GetDraft() bool            { return false }
func (r fakeRelease) GetPrerelease() bool       { return false }
func (r fakeRelease) GetPublishedAt() time.Time { return time.Time{} }
func (r fakeRelease) GetReleaseNotes() string   { return "" }
func (r fakeRelease) GetName() string           { return r.tag }
func (r fakeRelease) GetURL() string            { return "http://127.0.0.1/" + r.tag }
func (r fakeRelease) GetAssets() []selfupdate.SourceAsset {
	assets := make([]selfupdate.SourceAsset, len(r.assets))
	for i, a := range r.assets {
		assets[i] = a
	}
	return assets
}

// fakeSource serves signed releases from memory, so no test calls GitHub.
type fakeSource struct {
	releases []fakeRelease
	listErr  error
}

func (s *fakeSource) ListReleases(context.Context, selfupdate.Repository) ([]selfupdate.SourceRelease, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	releases := make([]selfupdate.SourceRelease, len(s.releases))
	for i, r := range s.releases {
		releases[i] = r
	}
	return releases, nil
}

func (s *fakeSource) DownloadReleaseAsset(_ context.Context, _ *selfupdate.Release, assetID int64) (io.ReadCloser, error) {
	for _, r := range s.releases {
		for _, a := range r.assets {
			if a.id == assetID {
				return io.NopCloser(bytes.NewReader(a.data)), nil
			}
		}
	}
	return nil, fmt.Errorf("asset %d not found", assetID)
}

// signedRelease builds a release whose archive holds a qui binary with the given
// content, plus checksums.txt and its signature.
func signedRelease(t *testing.T, key *ecdsa.PrivateKey, id int64, version string, binary []byte) fakeRelease {
	t.Helper()

	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "qui", Mode: 0o755, Size: int64(len(binary))}))
	_, err := tw.Write(binary)
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())

	archiveName := fmt.Sprintf("qui_%s_%s_%s.tar.gz", version, runtime.GOOS, runtime.GOARCH)
	checksums := fmt.Appendf(nil, "%x  %s\n", sha256.Sum256(archive.Bytes()), archiveName)

	return fakeRelease{id: id, tag: "v" + version, assets: []fakeAsset{
		{id: id*10 + 1, name: archiveName, data: archive.Bytes()},
		{id: id*10 + 2, name: releaseChecksumsAsset, data: checksums},
		{id: id*10 + 3, name: releaseChecksumsAsset + ".sig", data: signECDSA(t, key, checksums)},
	}}
}

type installFixture struct {
	updater *Updater
	source  *fakeSource
	binary  string
	dir     string
}

func newInstallFixture(t *testing.T) installFixture {
	t.Helper()

	certificate, key := generateECDSACertificate(t)
	dir := t.TempDir()
	binary := filepath.Join(dir, "qui")
	require.NoError(t, os.WriteFile(binary, []byte("qui 1.30.0"), 0o700))

	source := &fakeSource{releases: []fakeRelease{
		signedRelease(t, key, 3, "1.32.0", []byte("qui 1.32.0")),
		signedRelease(t, key, 2, "1.31.0", []byte("qui 1.31.0")),
		signedRelease(t, key, 1, "1.30.0", []byte("qui 1.30.0 again")),
	}}

	updater := NewUpdater(Config{Repository: "autobrr/qui", Version: "1.30.0", BinaryPath: binary})
	updater.source = source
	updater.certificate = certificate

	return installFixture{updater: updater, source: source, binary: binary, dir: dir}
}

func backupName(version string) string {
	return "qui-v" + version + backupSuffix()
}

func requireUnchanged(t *testing.T, f installFixture) {
	t.Helper()
	got, err := os.ReadFile(f.binary)
	require.NoError(t, err)
	require.Equal(t, "qui 1.30.0", string(got))
	require.NoFileExists(t, filepath.Join(f.dir, backupName("1.30.0")))
}

func TestInstallSwapsToRequestedTagAndKeepsBackup(t *testing.T) {
	f := newInstallFixture(t)
	older := filepath.Join(f.dir, backupName("1.29.0"))
	unrelated := filepath.Join(f.dir, "qui-notes.txt")
	require.NoError(t, os.WriteFile(older, []byte("qui 1.29.0"), 0o600))
	require.NoError(t, os.WriteFile(unrelated, []byte("keep"), 0o600))

	// The source also has v1.32.0; the dialog showed v1.31.0, so that is what installs.
	result, err := f.updater.Install(t.Context(), "v1.31.0")
	require.NoError(t, err)

	got, err := os.ReadFile(f.binary)
	require.NoError(t, err)
	require.Equal(t, "qui 1.31.0", string(got))

	backup := filepath.Join(f.dir, backupName("1.30.0"))
	saved, err := os.ReadFile(backup)
	require.NoError(t, err)
	require.Equal(t, "qui 1.30.0", string(saved))

	require.Equal(t, "1.31.0", result.Version)
	require.Empty(t, result.BackupError)
	if runtime.GOOS == "windows" {
		require.Equal(t, fmt.Sprintf(`cmd /c move /Y "%s" "%s"`, backup, f.binary), result.RollbackCommand)
	} else {
		require.Equal(t, fmt.Sprintf(`mv '%s' '%s'`, backup, f.binary), result.RollbackCommand)
	}

	require.NoFileExists(t, older)
	require.FileExists(t, unrelated)
}

func TestInstallRefusesTagThatIsNotNewer(t *testing.T) {
	f := newInstallFixture(t)

	_, err := f.updater.Install(t.Context(), "v1.30.0")
	require.ErrorIs(t, err, ErrNotNewer)
	requireUnchanged(t, f)
}

func TestInstallReportsMissingRelease(t *testing.T) {
	f := newInstallFixture(t)

	_, err := f.updater.Install(t.Context(), "v9.9.9")
	require.ErrorIs(t, err, ErrReleaseNotFound)
	requireUnchanged(t, f)
}

func TestInstallPassesSourceErrorThrough(t *testing.T) {
	f := newInstallFixture(t)
	f.source.listErr = errors.New("403 API rate limit exceeded")

	_, err := f.updater.Install(t.Context(), "v1.31.0")
	require.ErrorContains(t, err, "403 API rate limit exceeded")
	require.NotErrorIs(t, err, ErrSwap)
	requireUnchanged(t, f)
}

func TestInstallRejectsBadSignature(t *testing.T) {
	f := newInstallFixture(t)
	otherCertificate, _ := generateECDSACertificate(t)
	f.updater.certificate = otherCertificate

	_, err := f.updater.Install(t.Context(), "v1.31.0")
	require.ErrorIs(t, err, selfupdate.ErrECDSAValidationFailed)
	require.NotErrorIs(t, err, ErrSwap)
	requireUnchanged(t, f)
}

func TestInstallReportsSwapFailure(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a directory that the test cannot write to")
	}
	f := newInstallFixture(t)
	require.NoError(t, os.Chmod(f.dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(f.dir, 0o700) })

	_, err := f.updater.Install(t.Context(), "v1.31.0")
	require.ErrorIs(t, err, ErrSwap)
	requireUnchanged(t, f)
}

func TestInstallReportsBackupCheckFailure(t *testing.T) {
	f := newInstallFixture(t)
	older := filepath.Join(f.dir, backupName("1.29.0"))
	require.NoError(t, os.WriteFile(older, []byte("qui 1.29.0"), 0o600))

	backup := filepath.Join(f.dir, backupName("1.30.0"))
	statBackup = func(path string) (os.FileInfo, error) {
		require.Equal(t, backup, path)
		return nil, &os.PathError{Op: "stat", Path: path, Err: os.ErrPermission}
	}
	t.Cleanup(func() { statBackup = os.Stat })

	result, err := f.updater.Install(t.Context(), "v1.31.0")
	require.NoError(t, err)
	require.Equal(t, "1.31.0", result.Version)
	require.Empty(t, result.RollbackCommand)
	// The real cause, not "not found": the backup may exist.
	require.Equal(t, "stat "+backup+": permission denied", result.BackupError)
	// Without the new backup, the older one is the only way back.
	require.FileExists(t, older)
}

func TestInstallReportsReleaseWithoutAssetForThisPlatform(t *testing.T) {
	f := newInstallFixture(t)
	release := f.source.releases[1]
	release.assets[0].name = "qui_1.31.0_plan9_mips.tar.gz"
	f.source.releases[1] = release

	_, err := f.updater.Install(t.Context(), "v1.31.0")
	require.ErrorIs(t, err, ErrReleaseNotFound)
	requireUnchanged(t, f)
}

func TestShellQuoteKeepsPathLiteral(t *testing.T) {
	require.Equal(t, `'/opt/$qui/`+"`id`"+`/it'\''s qui'`, shellQuote("/opt/$qui/`id`/it's qui"))
}

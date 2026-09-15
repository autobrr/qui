// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
	internalqbittorrent "github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/sshpool"
	"github.com/autobrr/qui/internal/testutil/sshtest"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

// sshFixture is a handler wired to a real store and dialer, plus an in-process
// SSH server standing in for the instance's host.
type sshFixture struct {
	t         *testing.T
	router    chi.Router
	db        *database.DB
	store     *models.InstanceStore
	instance  *models.Instance
	server    *sshtest.Server
	clientKey string
}

func newSSHFixture(t *testing.T, name string, exec sshtest.ExecMode) *sshFixture {
	t.Helper()

	db := testdb.NewMigratedSQLite(t, name)
	store, err := models.NewInstanceStore(db, sshtest.EncryptionKey())
	require.NoError(t, err)

	clientPool, err := internalqbittorrent.NewClientPool(store, models.NewInstanceErrorStore(db), 60*time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { _ = clientPool.Close() })

	instance, err := store.Create(t.Context(), "remote", "http://127.0.0.1:1", "admin", "password", nil, nil, false, nil)
	require.NoError(t, err)

	handler := NewInstancesHandler(store, nil, nil, clientPool, nil, nil, sshpool.NewDialer(store))

	router := chi.NewRouter()
	router.Get("/api/instances", handler.ListInstances)
	router.Put("/api/instances/{instanceID}/ssh-credentials", handler.UpdateSSHCredentials)
	router.Delete("/api/instances/{instanceID}/ssh-credentials", handler.DeleteSSHCredentials)
	router.Post("/api/instances/{instanceID}/ssh-test", handler.TestSSHConnection)
	router.Post("/api/instances/{instanceID}/ssh-host-key", handler.ConfirmSSHHostKey)
	router.Post("/api/instances/{instanceID}/ssh-host-key/replace", handler.ReplaceSSHHostKey)

	return &sshFixture{
		t:         t,
		router:    router,
		db:        db,
		store:     store,
		instance:  instance,
		server:    sshtest.NewServer(t, sshtest.NewSigner(), exec),
		clientKey: sshtest.PrivateKey(""),
	}
}

func (f *sshFixture) do(method, path, body string) *httptest.ResponseRecorder {
	f.t.Helper()

	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	recorder := httptest.NewRecorder()
	f.router.ServeHTTP(recorder, httptest.NewRequestWithContext(f.t.Context(), method, fmt.Sprintf("/api/instances/%d%s", f.instance.ID, path), reader))
	return recorder
}

// putCredentials points the instance at the test SSH server.
func (f *sshFixture) putCredentials() {
	f.t.Helper()

	host, portText, err := net.SplitHostPort(f.server.Addr)
	require.NoError(f.t, err)
	port, err := strconv.Atoi(portText)
	require.NoError(f.t, err)

	body, err := json.Marshal(SSHCredentialsRequest{Host: host, Port: port, Username: "qui", PrivateKey: f.clientKey})
	require.NoError(f.t, err)

	response := f.do(http.MethodPut, "/ssh-credentials", string(body))
	require.Equal(f.t, http.StatusOK, response.Code, response.Body.String())
}

func (f *sshFixture) sshTest() SSHTestResponse {
	f.t.Helper()

	response := f.do(http.MethodPost, "/ssh-test", "")
	require.Equal(f.t, http.StatusOK, response.Code, response.Body.String())

	var body SSHTestResponse
	require.NoError(f.t, json.Unmarshal(response.Body.Bytes(), &body))
	return body
}

func hostKeyBody(key ssh.PublicKey) string {
	return fmt.Sprintf(`{"hostKey":%q}`, base64.StdEncoding.EncodeToString(key.Marshal()))
}

// endpointOf returns the host and port the fixture's credentials point at, for
// the tests that write a pin through the store directly.
func (f *sshFixture) endpointOf() (string, int) {
	f.t.Helper()

	stored, err := f.store.Get(f.t.Context(), f.instance.ID)
	require.NoError(f.t, err)
	return stored.SSHHost, stored.SSHPort
}

func TestSSHTestReportsFirstContact(t *testing.T) {
	f := newSSHFixture(t, "ssh-first-contact", sshtest.ExecGNU)
	f.putCredentials()

	body := f.sshTest()
	assert.Equal(t, string(sshpool.StatusUnpinned), body.Status)
	assert.Equal(t, ssh.FingerprintSHA256(f.server.HostKey), body.Fingerprint)
	assert.Equal(t, base64.StdEncoding.EncodeToString(f.server.HostKey.Marshal()), body.HostKey)
	assert.Equal(t, f.server.HostKey.Type(), body.KeyType)
	assert.Empty(t, body.PinnedFingerprint)
	require.NotNil(t, body.Capabilities)
	assert.True(t, body.Capabilities.SFTP)
	assert.True(t, body.Capabilities.Exec)
	assert.True(t, body.Capabilities.GNUUserland)
}

func TestConfirmHostKeyPinsIt(t *testing.T) {
	f := newSSHFixture(t, "ssh-confirm-pins", sshtest.ExecGNU)
	f.putCredentials()

	// Echo back exactly what the test reported, the way the UI will.
	presented := f.sshTest().HostKey
	confirm := f.do(http.MethodPost, "/ssh-host-key", fmt.Sprintf(`{"hostKey":%q}`, presented))
	require.Equal(t, http.StatusNoContent, confirm.Code, confirm.Body.String())

	body := f.sshTest()
	assert.Equal(t, string(sshpool.StatusPinned), body.Status)
	assert.Equal(t, ssh.FingerprintSHA256(f.server.HostKey), body.Fingerprint)
	require.NotNil(t, body.Capabilities)
	assert.True(t, body.Capabilities.SFTP)
}

func TestConfirmHostKeyTwiceConflicts(t *testing.T) {
	f := newSSHFixture(t, "ssh-confirm-twice", sshtest.ExecGNU)
	f.putCredentials()

	require.Equal(t, http.StatusNoContent, f.do(http.MethodPost, "/ssh-host-key", hostKeyBody(f.server.HostKey)).Code)

	second := f.do(http.MethodPost, "/ssh-host-key", hostKeyBody(f.server.HostKey))
	assert.Equal(t, http.StatusConflict, second.Code, "re-pinning must be asked for by name, not fallen into")
}

func TestReplaceHostKeyWithoutPinConflicts(t *testing.T) {
	f := newSSHFixture(t, "ssh-replace-unpinned", sshtest.ExecGNU)
	f.putCredentials()

	response := f.do(http.MethodPost, "/ssh-host-key/replace", hostKeyBody(f.server.HostKey))
	assert.Equal(t, http.StatusConflict, response.Code, "a replacement the user never compared is a silent TOFU")
}

func TestRotatedHostKeyMismatchesThenReplaces(t *testing.T) {
	f := newSSHFixture(t, "ssh-rotated-key", sshtest.ExecGNU)
	f.putCredentials()

	// The state a re-keyed host leaves behind: the pin holds the old key while
	// the host presents a new one.
	stale := sshtest.NewSigner().PublicKey()
	host, port := f.endpointOf()
	require.NoError(t, f.store.SetHostKeyPin(t.Context(), f.instance.ID, host, port, stale.Marshal()))

	mismatch := f.sshTest()
	assert.Equal(t, string(sshpool.StatusMismatch), mismatch.Status)
	assert.Equal(t, ssh.FingerprintSHA256(f.server.HostKey), mismatch.Fingerprint)
	assert.Equal(t, ssh.FingerprintSHA256(stale), mismatch.PinnedFingerprint)
	assert.Equal(t, stale.Type(), mismatch.PinnedKeyType)
	assert.Nil(t, mismatch.Capabilities, "a refused host key opens no session to probe")

	// Confirming the mismatched key through the first-pin route must not work.
	assert.Equal(t, http.StatusConflict, f.do(http.MethodPost, "/ssh-host-key", hostKeyBody(f.server.HostKey)).Code)

	replace := f.do(http.MethodPost, "/ssh-host-key/replace", hostKeyBody(f.server.HostKey))
	require.Equal(t, http.StatusNoContent, replace.Code, replace.Body.String())

	assert.Equal(t, string(sshpool.StatusPinned), f.sshTest().Status)
}

func TestReplaceRejectsKeyTheHostDoesNotPresent(t *testing.T) {
	f := newSSHFixture(t, "ssh-replace-other-key", sshtest.ExecGNU)
	f.putCredentials()
	require.Equal(t, http.StatusNoContent, f.do(http.MethodPost, "/ssh-host-key", hostKeyBody(f.server.HostKey)).Code)

	other := sshtest.NewSigner().PublicKey()
	response := f.do(http.MethodPost, "/ssh-host-key/replace", hostKeyBody(other))
	assert.Equal(t, http.StatusConflict, response.Code, "a pin is only ever written for a key the host actually presented")
}

func TestPinRejectsMalformedHostKey(t *testing.T) {
	f := newSSHFixture(t, "ssh-pin-malformed", sshtest.ExecGNU)
	f.putCredentials()

	assert.Equal(t, http.StatusBadRequest, f.do(http.MethodPost, "/ssh-host-key", `{"hostKey":"not base64!"}`).Code)
	assert.Equal(t, http.StatusBadRequest, f.do(http.MethodPost, "/ssh-host-key", `{"hostKey":"c3NoLWVkMjU1MTk="}`).Code)
}

func TestDeleteCredentialsKeepsThePin(t *testing.T) {
	f := newSSHFixture(t, "ssh-delete-credentials", sshtest.ExecGNU)
	f.putCredentials()
	require.Equal(t, http.StatusNoContent, f.do(http.MethodPost, "/ssh-host-key", hostKeyBody(f.server.HostKey)).Code)

	require.Equal(t, http.StatusNoContent, f.do(http.MethodDelete, "/ssh-credentials", "").Code)

	listed := f.listInstance()
	assert.True(t, listed.SSHHostKeyPinned, "the pin belongs to the host, not to the credentials")
	assert.Empty(t, listed.SSHUsername)
	// Without a key there is no usable remote, whatever the pin says.
	assert.Equal(t, string(models.FilesystemModeNone), listed.FilesystemMode)
}

func TestSSHTestWithoutCredentials(t *testing.T) {
	f := newSSHFixture(t, "ssh-test-no-credentials", sshtest.ExecGNU)

	response := f.do(http.MethodPost, "/ssh-test", "")
	assert.Equal(t, http.StatusBadRequest, response.Code)
}

func TestSSHTestReportsUnreachableHostAsResult(t *testing.T) {
	f := newSSHFixture(t, "ssh-test-unreachable", sshtest.ExecGNU)
	f.putCredentials()

	// Point the instance at a port nothing listens on.
	body, err := json.Marshal(SSHCredentialsRequest{Host: "127.0.0.1", Port: 1, Username: "qui", PrivateKey: f.clientKey})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, f.do(http.MethodPut, "/ssh-credentials", string(body)).Code)

	result := f.sshTest()
	assert.Equal(t, "error", result.Status)
	assert.NotEmpty(t, result.Error)
	assert.Empty(t, result.HostKey)
}

func TestUpdateCredentialsRejectsBadInput(t *testing.T) {
	f := newSSHFixture(t, "ssh-credentials-validation", sshtest.ExecGNU)

	tests := []struct {
		name string
		body string
	}{
		{name: "no host", body: fmt.Sprintf(`{"host":"","port":22,"username":"qui","privateKey":%q}`, f.clientKey)},
		{name: "port out of range", body: fmt.Sprintf(`{"host":"127.0.0.1","port":70000,"username":"qui","privateKey":%q}`, f.clientKey)},
		{name: "no username", body: fmt.Sprintf(`{"host":"127.0.0.1","port":22,"username":"","privateKey":%q}`, f.clientKey)},
		{name: "unparseable key", body: `{"host":"127.0.0.1","port":22,"username":"qui","privateKey":"not a key"}`},
		{name: "passphrase protected key", body: fmt.Sprintf(`{"host":"127.0.0.1","port":22,"username":"qui","privateKey":%q}`, sshtest.PrivateKey("hunter2"))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := f.do(http.MethodPut, "/ssh-credentials", tt.body)
			assert.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
		})
	}
}

// A cipher or database fault is not the submitter's fault, and its message is
// not theirs to read either.
func TestUpdateCredentialsReportsStoreFailureAsServerError(t *testing.T) {
	f := newSSHFixture(t, "ssh-credentials-store-failure", sshtest.ExecGNU)
	require.NoError(t, f.db.Close())

	body := fmt.Sprintf(`{"host":"127.0.0.1","port":22,"username":"qui","privateKey":%q}`, f.clientKey)
	response := f.do(http.MethodPut, "/ssh-credentials", body)
	assert.Equal(t, http.StatusInternalServerError, response.Code)
	assert.NotContains(t, response.Body.String(), "sql", "a driver error is not a user-facing message")
}

func TestInstanceResponseCarriesSSHFieldsWithoutKeyMaterial(t *testing.T) {
	f := newSSHFixture(t, "ssh-instance-response", sshtest.ExecGNU)
	f.putCredentials()
	require.Equal(t, http.StatusNoContent, f.do(http.MethodPost, "/ssh-host-key", hostKeyBody(f.server.HostKey)).Code)

	host, port := f.endpointOf()
	listed := f.listInstance()
	assert.Equal(t, host, listed.SSHHost)
	assert.Equal(t, port, listed.SSHPort)
	assert.Equal(t, "qui", listed.SSHUsername)
	assert.True(t, listed.SSHHostKeyPinned)
	assert.Equal(t, string(models.FilesystemModeRemote), listed.FilesystemMode)

	raw := f.listBody()
	assert.NotContains(t, raw, "BEGIN OPENSSH PRIVATE KEY", "no response may carry the private key")
	assert.NotContains(t, raw, "sshKey")
	assert.NotContains(t, raw, "sshHostKeyEncrypted")
}

func (f *sshFixture) listBody() string {
	f.t.Helper()

	recorder := httptest.NewRecorder()
	f.router.ServeHTTP(recorder, httptest.NewRequestWithContext(f.t.Context(), http.MethodGet, "/api/instances", nil))
	require.Equal(f.t, http.StatusOK, recorder.Code)
	return recorder.Body.String()
}

func (f *sshFixture) listInstance() InstanceResponse {
	f.t.Helper()

	var listed []InstanceResponse
	require.NoError(f.t, json.Unmarshal([]byte(f.listBody()), &listed))
	require.Len(f.t, listed, 1)
	return listed[0]
}

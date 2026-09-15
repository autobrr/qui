// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"
	"golang.org/x/crypto/ssh"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/sshpool"
)

// SSHCredentialsRequest is the endpoint and key qui dials the instance's host
// with. The key is write-only: no response ever carries it back.
type SSHCredentialsRequest struct {
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Username   string `json:"username"`
	PrivateKey string `json:"privateKey"`
}

// SSHHostKeyRequest carries the host public key the user confirmed, base64
// (standard encoding) over SSH wire format — the same bytes hostKey in
// SSHTestResponse was built from, echoed back so the server pins what the user
// actually saw.
type SSHHostKeyRequest struct {
	HostKey string `json:"hostKey"`
}

// SSHTestResponse reports what a dial found. Status "error" carries Error and
// nothing else: a host that will not answer has no key to show.
type SSHTestResponse struct {
	Status            string           `json:"status"`
	HostKey           string           `json:"hostKey,omitempty"`
	Fingerprint       string           `json:"fingerprint,omitempty"`
	KeyType           string           `json:"keyType,omitempty"`
	PinnedFingerprint string           `json:"pinnedFingerprint,omitempty"`
	PinnedKeyType     string           `json:"pinnedKeyType,omitempty"`
	Capabilities      *SSHCapabilities `json:"capabilities,omitempty"`
	Error             string           `json:"error,omitempty"`
}

// SSHCapabilities is what a probe of the host found it can do.
type SSHCapabilities struct {
	SFTP        bool `json:"sftp"`
	Statvfs     bool `json:"statvfs"`
	Hardlink    bool `json:"hardlink"`
	Limits      bool `json:"limits"`
	Exec        bool `json:"exec"`
	GNUUserland bool `json:"gnuUserland"`
}

// UpdateSSHCredentials stores the SSH endpoint and private key for an instance.
func (h *InstancesHandler) UpdateSSHCredentials(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	var req SSHCredentialsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if err := h.instanceStore.SetSSHCredentials(r.Context(), instanceID, req.Host, req.Port, req.Username, req.PrivateKey); err != nil {
		if errors.Is(err, models.ErrInstanceNotFound) {
			RespondError(w, http.StatusNotFound, "Instance not found")
			return
		}
		// Everything the store rejects here is a property of the submitted
		// endpoint or key, so its own message is what the user needs to fix.
		RespondError(w, http.StatusBadRequest, err.Error())
		return
	}

	RespondJSON(w, http.StatusOK, struct{}{})
}

// DeleteSSHCredentials forgets the key but keeps the pin: the pin belongs to
// the host, not to whoever last authenticated against it.
func (h *InstancesHandler) DeleteSSHCredentials(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	if err := h.instanceStore.ClearSSHCredentials(r.Context(), instanceID); err != nil {
		if errors.Is(err, models.ErrInstanceNotFound) {
			RespondError(w, http.StatusNotFound, "Instance not found")
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to clear SSH credentials")
		RespondError(w, http.StatusInternalServerError, "Failed to clear SSH credentials")
		return
	}

	RespondJSON(w, http.StatusNoContent, nil)
}

// TestSSHConnection dials the instance and reports the host key and what the
// host can do. A refused or unreachable host is a 200 with status "error", the
// same shape TestConnection uses: the browser is showing a result, not
// recovering from a failed request.
func (h *InstancesHandler) TestSSHConnection(w http.ResponseWriter, r *http.Request) {
	instance, ok := h.sshInstance(w, r)
	if !ok {
		return
	}

	report, err := h.sshDialer.Test(r.Context(), instance)
	if err != nil {
		if errors.Is(err, models.ErrSSHKeyNotConfigured) {
			RespondError(w, http.StatusBadRequest, "SSH credentials are not configured for this instance")
			return
		}
		RespondJSON(w, http.StatusOK, SSHTestResponse{Status: "error", Error: err.Error()})
		return
	}

	response := SSHTestResponse{
		Status:      string(report.Status),
		HostKey:     base64.StdEncoding.EncodeToString(report.HostKey.Marshal()),
		Fingerprint: ssh.FingerprintSHA256(report.HostKey),
		KeyType:     report.HostKey.Type(),
	}
	if report.PinnedKey != nil {
		response.PinnedFingerprint = ssh.FingerprintSHA256(report.PinnedKey)
		response.PinnedKeyType = report.PinnedKey.Type()
	}
	if report.Capabilities != nil {
		response.Capabilities = &SSHCapabilities{
			SFTP:        report.Capabilities.SFTP,
			Statvfs:     report.Capabilities.Statvfs,
			Hardlink:    report.Capabilities.Hardlink,
			Limits:      report.Capabilities.Limits,
			Exec:        report.Capabilities.Exec,
			GNUUserland: report.Capabilities.GNUUserland,
		}
	}

	RespondJSON(w, http.StatusOK, response)
}

// ConfirmSSHHostKey pins the host key for an instance that has none.
func (h *InstancesHandler) ConfirmSSHHostKey(w http.ResponseWriter, r *http.Request) {
	h.pinHostKey(w, r, false)
}

// ReplaceSSHHostKey pins a new host key over an existing one, the move a user
// makes after seeing a mismatch and deciding the host was legitimately re-keyed.
func (h *InstancesHandler) ReplaceSSHHostKey(w http.ResponseWriter, r *http.Request) {
	h.pinHostKey(w, r, true)
}

func (h *InstancesHandler) pinHostKey(w http.ResponseWriter, r *http.Request, replace bool) {
	instance, ok := h.sshInstance(w, r)
	if !ok {
		return
	}

	var req SSHHostKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	hostKey, err := base64.StdEncoding.DecodeString(req.HostKey)
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Host key is not valid base64")
		return
	}
	if _, err := ssh.ParsePublicKey(hostKey); err != nil {
		RespondError(w, http.StatusBadRequest, "Host key is not in SSH wire format")
		return
	}

	// Re-dial rather than trust the echoed key: pinning what the host presents
	// right now is the trust-on-first-use the confirmation step exists to
	// replace, and it is also what makes the flow stateless between requests.
	if err := h.sshDialer.Confirm(r.Context(), instance, hostKey); err != nil {
		_, mismatch := errors.AsType[*sshpool.MismatchError](err)
		switch {
		case errors.Is(err, models.ErrSSHKeyNotConfigured):
			RespondError(w, http.StatusBadRequest, "SSH credentials are not configured for this instance")
		case mismatch:
			RespondError(w, http.StatusConflict, "Host presented a different key than the one being confirmed")
		default:
			RespondError(w, http.StatusBadGateway, err.Error())
		}
		return
	}

	pin := h.instanceStore.SetHostKeyPin
	if replace {
		pin = h.instanceStore.ReplaceHostKeyPin
	}

	if err := pin(r.Context(), instance.ID, instance.SSHHost, instance.SSHPort, hostKey); err != nil {
		switch {
		case errors.Is(err, models.ErrInstanceNotFound):
			RespondError(w, http.StatusNotFound, "Instance not found")
		case errors.Is(err, models.ErrSSHHostKeyAlreadyPinned),
			errors.Is(err, models.ErrSSHHostKeyNotPinned),
			errors.Is(err, models.ErrSSHEndpointChanged):
			RespondError(w, http.StatusConflict, err.Error())
		default:
			log.Error().Err(err).Int("instanceID", instance.ID).Msg("Failed to pin SSH host key")
			RespondError(w, http.StatusInternalServerError, "Failed to pin SSH host key")
		}
		return
	}

	RespondJSON(w, http.StatusNoContent, nil)
}

// sshInstance loads the instance for a dialing endpoint, answering the request
// itself on every failure. The nil dialer is reachable only from tests that
// construct the handler without one.
func (h *InstancesHandler) sshInstance(w http.ResponseWriter, r *http.Request) (*models.Instance, bool) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return nil, false
	}

	if h.sshDialer == nil {
		RespondError(w, http.StatusInternalServerError, "SSH support is not available")
		return nil, false
	}

	instance, err := h.instanceStore.Get(r.Context(), instanceID)
	if err != nil {
		if errors.Is(err, models.ErrInstanceNotFound) {
			RespondError(w, http.StatusNotFound, "Instance not found")
			return nil, false
		}
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to fetch instance")
		RespondError(w, http.StatusInternalServerError, "Failed to fetch instance")
		return nil, false
	}

	return instance, true
}

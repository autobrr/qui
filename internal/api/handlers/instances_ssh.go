// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/rs/zerolog/log"
	"golang.org/x/crypto/ssh"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/orphanscan"
	"github.com/autobrr/qui/internal/sshpool"
)

type SSHCredentialsRequest struct {
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Username   string `json:"username"`
	PrivateKey string `json:"privateKey"`
}

type SSHHostKeyRequest struct {
	HostKey string `json:"hostKey"`
}

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

type SSHCapabilities struct {
	SFTP        bool `json:"sftp"`
	Statvfs     bool `json:"statvfs"`
	Hardlink    bool `json:"hardlink"`
	Limits      bool `json:"limits"`
	Exec        bool `json:"exec"`
	GNUUserland bool `json:"gnuUserland"`
}

func (h *InstancesHandler) UpdateSSHCredentials(w http.ResponseWriter, r *http.Request) {
	instanceID, err := parseInstanceID(w, r)
	if err != nil {
		return
	}

	var req SSHCredentialsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	instance, err := h.instanceStore.Get(r.Context(), instanceID)
	if err != nil {
		if errors.Is(err, models.ErrInstanceNotFound) {
			RespondError(w, http.StatusNotFound, "Instance not found")
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to fetch instance")
		RespondError(w, http.StatusInternalServerError, "Failed to fetch instance")
		return
	}
	after := *instance
	after.SSHHost, after.SSHPort, after.SSHUsername, after.SSHKeyEncrypted = req.Host, req.Port, req.Username, "set"
	endpointChanged := models.SSHEndpointChanged(instance, req.Host, req.Port)
	if endpointChanged {
		after.SSHHostKeyEncrypted = ""
	}
	if (instance.UseHardlinks || instance.UseReflinks) && endpointChanged && !models.FilesystemCapabilitiesOf(&after).Write {
		RespondError(w, http.StatusBadRequest, "Disable hardlink or reflink mode on this instance before changing its SSH host or port")
		return
	}
	if h.refuseRouteBreak(w, r, instance, &after) {
		return
	}

	err = h.changeSSH(r.Context(), instanceID, func() error {
		if err := h.instanceStore.SetSSHCredentials(r.Context(), instanceID, req.Host, req.Port, req.Username, req.PrivateKey); err != nil {
			return err
		}
		h.sshPool.Invalidate(instanceID)
		return nil
	})
	if err != nil {
		switch {
		case errors.Is(err, orphanscan.ErrDeletionInProgress):
			RespondError(w, http.StatusConflict, sshChangeDuringDeletionMessage)
		case errors.Is(err, models.ErrInstanceNotFound):
			RespondError(w, http.StatusNotFound, "Instance not found")
		case errors.Is(err, models.ErrInvalidSSHCredentials):
			RespondError(w, http.StatusBadRequest, strings.TrimPrefix(err.Error(), models.ErrInvalidSSHCredentials.Error()+": "))
		default:
			log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to store SSH credentials")
			RespondError(w, http.StatusInternalServerError, "Failed to store SSH credentials")
		}
		return
	}

	RespondJSON(w, http.StatusNoContent, nil)
}

func (h *InstancesHandler) DeleteSSHCredentials(w http.ResponseWriter, r *http.Request) {
	instanceID, err := parseInstanceID(w, r)
	if err != nil {
		return
	}

	instance, err := h.instanceStore.Get(r.Context(), instanceID)
	if err != nil {
		if errors.Is(err, models.ErrInstanceNotFound) {
			RespondError(w, http.StatusNotFound, "Instance not found")
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to fetch instance")
		RespondError(w, http.StatusInternalServerError, "Failed to fetch instance")
		return
	}
	withoutCredentials := *instance
	withoutCredentials.SSHUsername = ""
	withoutCredentials.SSHKeyEncrypted = ""
	withoutCredentials.SSHTier = models.SSHTierSFTP
	withoutCredentials.SSHHardlink = nil
	if (instance.UseHardlinks || instance.UseReflinks) && !models.FilesystemCapabilitiesOf(&withoutCredentials).Write {
		RespondError(w, http.StatusBadRequest, "Disable hardlink or reflink mode on this instance before removing its SSH credentials")
		return
	}

	err = h.changeSSH(r.Context(), instanceID, func() error {
		if err := h.instanceStore.ClearSSHCredentials(r.Context(), instanceID); err != nil {
			return err
		}
		h.sshPool.Invalidate(instanceID)
		return nil
	})
	if err != nil {
		switch {
		case errors.Is(err, orphanscan.ErrDeletionInProgress):
			RespondError(w, http.StatusConflict, sshChangeDuringDeletionMessage)
		case errors.Is(err, models.ErrInstanceNotFound):
			RespondError(w, http.StatusNotFound, "Instance not found")
		default:
			log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to clear SSH credentials")
			RespondError(w, http.StatusInternalServerError, "Failed to clear SSH credentials")
		}
		return
	}

	RespondJSON(w, http.StatusNoContent, nil)
}

func (h *InstancesHandler) TestSSHConnection(w http.ResponseWriter, r *http.Request) {
	instance, ok := h.loadSSHInstance(w, r)
	if !ok {
		return
	}

	report, err := h.sshDialer.Test(r.Context(), instance)
	if err != nil {
		switch {
		case errors.Is(err, models.ErrSSHKeyNotConfigured):
			RespondError(w, http.StatusBadRequest, "SSH credentials are not configured for this instance")
		case r.Context().Err() != nil:
		case errors.Is(err, sshpool.ErrConnect):
			RespondJSON(w, http.StatusOK, SSHTestResponse{Status: "error", Error: err.Error()})
		default:
			log.Error().Err(err).Int("instanceID", instance.ID).Msg("Failed to read SSH credentials for SSH test")
			RespondJSON(w, http.StatusOK, SSHTestResponse{Status: "error", Error: "Failed to read SSH credentials"})
		}
		return
	}

	response := SSHTestResponse{Status: string(report.Status)}
	if report.HostKey != nil {
		response.HostKey = base64.StdEncoding.EncodeToString(report.HostKey.Marshal())
		response.Fingerprint = ssh.FingerprintSHA256(report.HostKey)
		response.KeyType = report.HostKey.Type()
	}
	if report.PinnedKey != nil {
		response.PinnedFingerprint = ssh.FingerprintSHA256(report.PinnedKey)
		response.PinnedKeyType = report.PinnedKey.Type()
	}
	if report.Capabilities != nil {
		h.storeSSHTier(r.Context(), instance, report.Capabilities)
		h.sshPool.Invalidate(instance.ID)
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

func (h *InstancesHandler) ConfirmSSHHostKey(w http.ResponseWriter, r *http.Request) {
	h.pinHostKey(w, r, false)
}

func (h *InstancesHandler) ReplaceSSHHostKey(w http.ResponseWriter, r *http.Request) {
	h.pinHostKey(w, r, true)
}

func (h *InstancesHandler) pinHostKey(w http.ResponseWriter, r *http.Request, replace bool) {
	instance, ok := h.loadSSHInstance(w, r)
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

	switch pinned := instance.SSHHostKeyEncrypted != ""; {
	case pinned && !replace:
		RespondError(w, http.StatusConflict, models.ErrSSHHostKeyAlreadyPinned.Error())
		return
	case !pinned && replace:
		RespondError(w, http.StatusConflict, models.ErrSSHHostKeyNotPinned.Error())
		return
	}

	pinned := *instance
	pinned.SSHHostKeyEncrypted = "set"
	if h.refuseRouteBreak(w, r, instance, &pinned) {
		return
	}

	capabilities, err := h.sshDialer.Confirm(r.Context(), instance, hostKey)
	if err != nil {
		_, mismatch := errors.AsType[*sshpool.MismatchError](err)
		switch {
		case errors.Is(err, models.ErrSSHKeyNotConfigured):
			RespondError(w, http.StatusBadRequest, "SSH credentials are not configured for this instance")
		case mismatch:
			RespondError(w, http.StatusConflict, "Host presented a different key than the one being confirmed")
		case r.Context().Err() != nil:
		case errors.Is(err, sshpool.ErrConnect):
			log.Debug().Err(err).Int("instanceID", instance.ID).Msg("SSH host key confirmation could not connect")
			RespondError(w, http.StatusBadGateway, "Could not connect to the SSH host")
		default:
			log.Error().Err(err).Int("instanceID", instance.ID).Msg("Failed to read SSH credentials for host key confirmation")
			RespondError(w, http.StatusInternalServerError, "Failed to read SSH credentials")
		}
		return
	}

	store := func() error {
		pin := h.instanceStore.SetHostKeyPin
		if replace {
			pin = h.instanceStore.ReplaceHostKeyPin
		}
		if err := pin(r.Context(), instance.ID, instance.SSHHost, instance.SSHPort, hostKey); err != nil {
			return err
		}
		if capabilities != nil {
			if pinned, err := h.instanceStore.Get(r.Context(), instance.ID); err != nil {
				log.Warn().Err(err).Int("instanceID", instance.ID).Msg("Failed to reload the instance after pinning the host key")
			} else {
				probed := *instance
				probed.SSHHostKeyEncrypted = pinned.SSHHostKeyEncrypted
				h.storeSSHTier(r.Context(), &probed, capabilities)
			}
		}
		h.sshPool.Invalidate(instance.ID)
		return nil
	}
	if replace {
		err = h.changeSSH(r.Context(), instance.ID, store)
	} else {
		err = store()
	}
	if err != nil {
		switch {
		case errors.Is(err, orphanscan.ErrDeletionInProgress):
			RespondError(w, http.StatusConflict, sshChangeDuringDeletionMessage)
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

func (h *InstancesHandler) storeSSHTier(ctx context.Context, probed *models.Instance, capabilities *sshpool.Capabilities) {
	if err := h.instanceStore.SetSSHProbe(ctx, probed, capabilities.Tier(), capabilities.Hardlink); err != nil {
		log.Warn().Err(err).Int("instanceID", probed.ID).Msg("Failed to store the SSH probe")
	}
}

func (h *InstancesHandler) loadSSHInstance(w http.ResponseWriter, r *http.Request) (*models.Instance, bool) {
	instanceID, err := parseInstanceID(w, r)
	if err != nil {
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

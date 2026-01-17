// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/singleflight"

	"github.com/autobrr/qui/internal/models"
)

// InstanceProvider abstracts instance store access
type InstanceProvider interface {
	Get(ctx context.Context, id int) (*models.Instance, error)
}

// SyncManager abstracts qBittorrent operations
type SyncManager interface {
	GetTorrents(ctx context.Context, instanceID int, filter qbt.TorrentFilterOptions) ([]qbt.Torrent, error)
	GetTorrentFiles(ctx context.Context, instanceID int, hash string) (*qbt.TorrentFiles, error)
	GetTorrentProperties(ctx context.Context, instanceID int, hash string) (*qbt.TorrentProperties, error)
	ExportTorrent(ctx context.Context, instanceID int, hash string) ([]byte, string, string, error)
	AddTorrent(ctx context.Context, instanceID int, fileContent []byte, options map[string]string) error
	BulkAction(ctx context.Context, instanceID int, hashes []string, action string) error
	DeleteTorrents(ctx context.Context, instanceID int, hashes []string, deleteFiles bool) error
	GetCategories(ctx context.Context, instanceID int) (map[string]qbt.Category, error)
	CreateCategory(ctx context.Context, instanceID int, name, path string) error
	HasTorrentByAnyHash(ctx context.Context, instanceID int, hashes []string) (*qbt.Torrent, bool, error)
}

// Service handles torrent transfers between instances
type Service struct {
	store         *models.TransferStore
	instanceStore InstanceProvider
	syncManager   SyncManager

	// Background worker
	workerCtx    context.Context
	workerCancel context.CancelFunc
	workerWg     sync.WaitGroup
	queue        chan int64

	// Category creation deduplication
	createdCategories     sync.Map
	categoryCreationGroup singleflight.Group

	// Configuration
	workerCount      int
	recoveryInterval time.Duration
}

var (
	ErrTransferNotFound = errors.New("transfer not found")
)

// New creates a new transfer service
func New(
	store *models.TransferStore,
	instanceStore InstanceProvider,
	syncManager SyncManager,
) *Service {
	return &Service{
		store:            store,
		instanceStore:    instanceStore,
		syncManager:      syncManager,
		queue:            make(chan int64, 100),
		workerCount:      2,
		recoveryInterval: 30 * time.Second,
	}
}

// Start initializes the service and recovers interrupted transfers
func (s *Service) Start(ctx context.Context) {
	s.workerCtx, s.workerCancel = context.WithCancel(ctx)

	// Recover interrupted transfers from previous run
	s.recoverInterrupted()

	// Start worker goroutines
	for i := 0; i < s.workerCount; i++ {
		s.workerWg.Go(func() {
			s.worker(i)
		})
	}

	// Start periodic recovery goroutine
	s.workerWg.Go(func() {
		s.periodicRecovery()
	})

	log.Info().Int("workers", s.workerCount).Msg("[TRANSFER] Service started")
}

// Stop gracefully shuts down the service
func (s *Service) Stop() {
	if s.workerCancel != nil {
		s.workerCancel()
	}
	s.workerWg.Wait()
	log.Info().Msg("[TRANSFER] Service stopped")
}

// QueueTransfer creates a new transfer and queues it for processing
func (s *Service) QueueTransfer(ctx context.Context, req *TransferRequest) (*models.Transfer, error) {
	// Validate request
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Check for existing non-terminal transfer for this hash
	existing, err := s.store.GetByHash(ctx, req.TorrentHash)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if existing != nil {
		return nil, ErrTransferAlreadyExists
	}

	// Create transfer record
	t := &models.Transfer{
		SourceInstanceID: req.SourceInstanceID,
		TargetInstanceID: req.TargetInstanceID,
		TorrentHash:      req.TorrentHash,
		TorrentName:      req.TorrentHash, // Will be updated during prepare
		State:            models.TransferStatePending,
		DeleteFromSource: req.DeleteFromSource,
		PreserveCategory: req.PreserveCategory,
		PreserveTags:     req.PreserveTags,
		PathMappings:     req.PathMappings,
	}

	created, err := s.store.Create(ctx, t)
	if err != nil {
		return nil, err
	}

	// Queue for processing; if full, periodic recovery will pick it up
	s.tryEnqueue(created.ID)

	return created, nil
}

// MoveTorrent is a convenience method for moving a torrent between instances
func (s *Service) MoveTorrent(ctx context.Context, req *MoveRequest) (*models.Transfer, error) {
	return s.QueueTransfer(ctx, &TransferRequest{
		SourceInstanceID: req.SourceInstanceID,
		TargetInstanceID: req.TargetInstanceID,
		TorrentHash:      req.Hash,
		PathMappings:     req.PathMappings,
		DeleteFromSource: req.DeleteFromSource,
		PreserveCategory: req.PreserveCategory,
		PreserveTags:     req.PreserveTags,
	})
}

// GetTransfer retrieves a transfer by ID
func (s *Service) GetTransfer(ctx context.Context, id int64) (*models.Transfer, error) {
	t, err := s.store.Get(ctx, id)
	if err == sql.ErrNoRows {
		return nil, ErrTransferNotFound
	} else if err != nil {
		return nil, err
	}
	return t, nil
}

// CancelTransfer attempts to cancel a pending transfer
func (s *Service) CancelTransfer(ctx context.Context, id int64) error {
	t, err := s.store.Get(ctx, id)
	if err != nil {
		return err
	}

	// Only pending transfers can be cancelled
	if t.State != models.TransferStatePending {
		return ErrCannotCancel
	}

	return s.store.UpdateState(ctx, id, models.TransferStateCancelled, "cancelled by user")
}

// ListTransfers returns transfers with optional filtering
func (s *Service) ListTransfers(ctx context.Context, opts ListOptions) ([]*models.Transfer, error) {
	if opts.Limit == 0 {
		opts.Limit = 50
	}

	if len(opts.States) > 0 {
		return s.store.ListByStates(ctx, opts.States, opts.Limit, opts.Offset)
	}

	if opts.InstanceID != nil {
		return s.store.ListByInstance(ctx, *opts.InstanceID, opts.Limit, opts.Offset)
	}

	return s.store.ListRecent(ctx, opts.Limit, opts.Offset)
}

// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseedroute

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/fsutil"
	"github.com/autobrr/qui/pkg/pathcmp"
)

var ErrCannotSeeFolder = errors.New("cannot see the folder")

func (s *Service) Lists(ctx context.Context, instanceID int, dir, name string, mode qbt.DirectoryContentMode) (bool, error) {
	client, err := s.clients.GetClient(ctx, instanceID)
	if err != nil {
		return false, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	content, err := client.ListDirectoryCtx(ctx, dir, mode, false)
	return ListingHas(content, err, dir, name)
}

func ListingHas(content any, err error, dir, name string) (bool, error) {
	switch {
	case errors.Is(err, qbt.ErrUnexpectedStatus):
		return false, fmt.Errorf("%w %s: %w", ErrCannotSeeFolder, dir, err)
	case err != nil:
		return false, fmt.Errorf("%w: list %s: %w", ErrUnavailable, dir, err)
	}
	entries, _ := content.([]string)
	return slices.ContainsFunc(entries, func(entry string) bool {
		entry = strings.TrimRight(entry, `/\`)
		last := entry[strings.LastIndexAny(entry, `/\`)+1:]
		if pathcmp.IsWindowsDriveAbs(pathcmp.NormalizePath(entry)) {
			return strings.EqualFold(last, name)
		}
		return last == name
	}), nil
}

type TestOutcome string

const (
	TestSees                TestOutcome = "sees"
	TestSeesFolderNotMarker TestOutcome = "seesFolderNotMarker"
	TestCannotSeeFolder     TestOutcome = "cannotSeeFolder"
	TestCannotWrite         TestOutcome = "cannotWrite"
)

type TestRow struct {
	BaseDir    string      `json:"baseDir"`
	ListedPath string      `json:"listedPath"`
	Result     TestOutcome `json:"result"`
	Error      string      `json:"error,omitempty"`
}

type TestResult struct {
	Unavailable string    `json:"crossSeedUnavailable,omitempty"`
	Rows        []TestRow `json:"rows"`
}

func (s *Service) Test(ctx context.Context, route *models.CrossSeedRoute) (*TestResult, error) {
	backend, source, err := s.backends.Require(ctx, route.SourceInstanceID, models.CapabilityWrite)
	switch {
	case errors.Is(err, fsops.ErrNotCapable):
		return nil, broken("The Source instance needs Local Filesystem Access to route its cross-seeds")
	case err != nil:
		return nil, err
	case models.FilesystemAccessMode(source) == models.FilesystemModeRemote:
		return nil, broken("%s is a remote instance and cannot route its cross-seeds", source.Name)
	}
	rows, err := cleanRows(backend.Paths(), source, route.Rows)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if row.MappedPath != "" && !pathcmp.IsAbsolute(row.MappedPath) {
			return nil, broken("mapped path %s for %s is not an absolute path", row.MappedPath, row.BaseDir)
		}
	}
	crossSeed, err := s.instances.Get(ctx, route.CrossSeedInstanceID)
	if err != nil {
		return nil, err
	}
	unavailable := &TestResult{Rows: []TestRow{}}
	if err := s.CheckAvailable(ctx, crossSeed.ID, true); err != nil {
		if !errors.Is(err, ErrUnavailable) {
			return nil, err
		}
		unavailable.Unavailable = CrossSeedUnavailable(err).Error()
		return unavailable, nil
	}

	result := &TestResult{Rows: make([]TestRow, len(rows))}
	for i, row := range rows {
		listed := row.MappedPath
		if listed == "" {
			listed = row.BaseDir
		}
		result.Rows[i] = TestRow{BaseDir: row.BaseDir, ListedPath: listed}
		outcome, err := s.testRow(ctx, backend, row.BaseDir, listed, crossSeed.ID)
		if errors.Is(err, ErrUnavailable) {
			unavailable.Unavailable = "Cross-seed instance unavailable: " + crossSeed.Name + ": " + strings.TrimPrefix(err.Error(), ErrUnavailable.Error()+": ")
			return unavailable, nil
		}
		if err != nil && ctx.Err() != nil {
			return nil, ctx.Err()
		}
		result.Rows[i].Result = outcome
		if err != nil {
			result.Rows[i].Error = err.Error()
		}
	}
	return result, nil
}

func (s *Service) testRow(ctx context.Context, backend fsops.Backend, baseDir, listed string, crossSeedID int) (TestOutcome, error) {
	if err := backend.MkdirAll(ctx, baseDir, fsutil.ContentDirMode); err != nil {
		return TestCannotWrite, err
	}
	name := "qui-route-test-" + rand.Text()
	marker := backend.Paths().Join(baseDir, name)
	if err := backend.Mkdir(ctx, marker, fsutil.ContentDirMode); err != nil {
		return TestCannotWrite, err
	}
	defer func() {
		removeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if err := backend.Remove(removeCtx, marker, fsops.RemoveOptions{}); err != nil {
			log.Warn().Err(err).Str("marker", marker).Msg("Failed to remove the cross-seed route test marker")
		}
	}()

	found, err := s.Lists(ctx, crossSeedID, listed, name, qbt.DirectoryContentDirs)
	switch {
	case errors.Is(err, ErrCannotSeeFolder):
		return TestCannotSeeFolder, nil
	case err != nil:
		return "", err
	case found:
		return TestSees, nil
	}
	return TestSeesFolderNotMarker, nil
}

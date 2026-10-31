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

// ErrCannotSeeFolder marks a folder that the qBittorrent of an instance
// refuses to list.
var ErrCannotSeeFolder = errors.New("cannot see the folder")

// Lists reports whether the qBittorrent directory listing of dir on
// instanceID holds an entry named name, as ListingHas reads it.
func (s *Service) Lists(ctx context.Context, instanceID int, dir, name string, mode qbt.DirectoryContentMode) (bool, error) {
	client, err := s.clients.GetClient(ctx, instanceID)
	if err != nil {
		return false, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	content, err := client.ListDirectoryCtx(ctx, dir, mode, false)
	return ListingHas(content, err, dir, name)
}

// ListingHas reads the answer of a qBittorrent directory listing of dir, and
// reports whether it holds an entry named name. It compares the last path
// element of each entry, with either separator, and ignores case on a
// drive-letter entry. An answer other than 200 matches ErrCannotSeeFolder. Any
// other failure, such as a transport error or a qBittorrent version older than
// 5.0, matches ErrUnavailable.
func ListingHas(content any, err error, dir, name string) (bool, error) {
	switch {
	// go-qbittorrent puts the status code only in the text, so a 404 and a
	// 400 look the same here.
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

// TestOutcome is the answer of a route test for one base folder.
type TestOutcome string

const (
	TestSees                TestOutcome = "sees"
	TestSeesFolderNotMarker TestOutcome = "seesFolderNotMarker"
	TestCannotSeeFolder     TestOutcome = "cannotSeeFolder"
	// TestCannotWrite means that qui cannot write to the base folder on the
	// Source instance.
	TestCannotWrite TestOutcome = "cannotWrite"
)

// TestRow is the test answer for one base folder.
type TestRow struct {
	BaseDir string `json:"baseDir"`
	// ListedPath is the folder that qui lists on the Cross-seed instance.
	ListedPath string      `json:"listedPath"`
	Result     TestOutcome `json:"result"`
	// Error is the cause of a cannotWrite result.
	Error string `json:"error,omitempty"`
}

// TestResult is the answer of a route test.
type TestResult struct {
	// Unavailable is set, and Rows is empty, when the Cross-seed instance is
	// unavailable.
	Unavailable string    `json:"crossSeedUnavailable,omitempty"`
	Rows        []TestRow `json:"rows"`
}

// Test checks each base folder of the Source instance with the rows of a
// route that is not saved yet. qui writes a marker folder in the base folder
// and asks the Cross-seed instance's qBittorrent if it lists the marker at the
// mapped path. The test always removes the marker and keeps the base folder.
func (s *Service) Test(ctx context.Context, route *models.CrossSeedRoute) (*TestResult, error) {
	backend, source, err := s.backends.Require(ctx, route.SourceInstanceID, models.CapabilityWrite)
	switch {
	case errors.Is(err, fsops.ErrNotCapable):
		return nil, broken("The Source instance needs Local Filesystem Access to route its cross-seeds")
	case err != nil:
		return nil, err
	// Only local and remote mode grant Write.
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
		// A request that ended is not a write failure of this folder.
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

// testRow writes a marker in baseDir and looks for it in listed on the
// Cross-seed instance. An error that matches ErrUnavailable stops the test.
func (s *Service) testRow(ctx context.Context, backend fsops.Backend, baseDir, listed string, crossSeedID int) (TestOutcome, error) {
	// Cross-seed link mode creates a missing base folder with the same mode,
	// so the umask decides who else can read it.
	if err := backend.MkdirAll(ctx, baseDir, fsutil.ContentDirMode); err != nil {
		return TestCannotWrite, err
	}
	name := "qui-route-test-" + rand.Text()
	marker := backend.Paths().Join(baseDir, name)
	if err := backend.Mkdir(ctx, marker, fsutil.ContentDirMode); err != nil {
		return TestCannotWrite, err
	}
	defer func() {
		// Remove the marker also when ctx ran out during the listing.
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

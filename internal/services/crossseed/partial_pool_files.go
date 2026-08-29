// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/go-torrent/metainfo"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/hardlinktree"
	"github.com/autobrr/qui/pkg/pathutil"
	"github.com/autobrr/qui/pkg/stringutils"
)

type partialPoolFileDescriptor struct {
	Index            int
	RelativePath     string
	RootStrippedPath string
	SizeBytes        int64
	PiecesRoot       string
}

type partialPoolV2Leaf struct {
	length int64
	root   string
}

// buildPartialPoolFileDescriptors joins v2 roots to files by exact path and
// length. File-tree traversal order is never used as an identity signal.
func buildPartialPoolFileDescriptors(info *metainfo.Info) ([]partialPoolFileDescriptor, error) {
	if info == nil {
		return nil, errors.New("torrent info is required")
	}
	descriptorInfo := *info
	if info.HasV2() && len(info.Files) == 0 {
		descriptorInfo.Files = info.UpvertedFiles()
	}
	files := BuildTorrentFilesFromInfo(stringutils.SanitizeUTF8(info.BestName()), descriptorInfo)
	if len(files) == 0 {
		return nil, errors.New("torrent has no files")
	}

	roots := partialPoolV2Roots(info)
	descriptors := make([]partialPoolFileDescriptor, 0, len(files))
	seen := make(map[string]int, len(files))
	for _, file := range files {
		relativePath, ok := safeTorrentRelativeFilePath(file.Name)
		if !ok {
			return nil, fmt.Errorf("unsafe torrent file path %q", file.Name)
		}
		stripped := stripTorrentRoot(relativePath, files)
		if stripped == "" {
			return nil, fmt.Errorf("empty root-stripped torrent path %q", file.Name)
		}
		key := stripped
		previous, duplicate := seen[key]
		if !duplicate {
			seen[key] = len(descriptors)
		} else {
			descriptors[previous].PiecesRoot = ""
		}

		descriptor := partialPoolFileDescriptor{
			Index:            file.Index,
			RelativePath:     relativePath,
			RootStrippedPath: stripped,
			SizeBytes:        file.Size,
		}
		if !duplicate {
			if leaf, found := roots[stripped]; found && leaf.length == file.Size && file.Size > 0 {
				descriptor.PiecesRoot = leaf.root
			}
		}
		descriptors = append(descriptors, descriptor)
	}
	return descriptors, nil
}

func partialPoolV2Roots(info *metainfo.Info) map[string]partialPoolV2Leaf {
	if info == nil || !info.HasV2() {
		return nil
	}
	roots := make(map[string]partialPoolV2Leaf)
	invalid := make(map[string]struct{})
	var walk func(metainfo.FileTree, []string)
	walk = func(node metainfo.FileTree, parts []string) {
		if len(node.Dir) > 0 {
			for name, child := range node.Dir {
				if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
					continue
				}
				component := pathutil.TorrentPathComponent(stringutils.SanitizeUTF8(name))
				if component == "" || component == "." || component == ".." || strings.ContainsAny(component, `/\`) {
					continue
				}
				walk(child, append(parts, component))
			}
			return
		}
		if len(parts) == 0 || node.File.Length <= 0 || len(node.File.PiecesRoot) != 32 {
			return
		}
		filePath := strings.Join(parts, "/")
		if _, exists := roots[filePath]; exists {
			invalid[filePath] = struct{}{}
			return
		}
		roots[filePath] = partialPoolV2Leaf{
			length: node.File.Length,
			root:   hex.EncodeToString([]byte(node.File.PiecesRoot)),
		}
	}
	walk(info.FileTree, nil)
	for filePath := range invalid {
		delete(roots, filePath)
	}
	return roots
}

func stripTorrentRoot(relativePath string, files qbt.TorrentFiles) string {
	root := detectCommonRoot(files)
	if root == "" {
		return relativePath
	}
	prefix := root + "/"
	if !strings.HasPrefix(relativePath, prefix) {
		return relativePath
	}
	return strings.TrimPrefix(relativePath, prefix)
}

func buildPartialPoolAdmissionFiles(descriptors []partialPoolFileDescriptor, files qbt.TorrentFiles, materializedPaths map[string]struct{}) ([]models.CrossSeedPartialPoolMemberFile, int64, error) {
	if len(descriptors) != len(files) {
		return nil, 0, fmt.Errorf("torrent file count changed after add: metainfo=%d qbittorrent=%d", len(descriptors), len(files))
	}
	descriptorByIndex := make(map[int]partialPoolFileDescriptor, len(descriptors))
	for _, descriptor := range descriptors {
		if _, duplicate := descriptorByIndex[descriptor.Index]; duplicate {
			return nil, 0, fmt.Errorf("duplicate metainfo file index %d", descriptor.Index)
		}
		descriptorByIndex[descriptor.Index] = descriptor
	}

	rows := make([]models.CrossSeedPartialPoolMemberFile, 0, len(files))
	var missingBytes int64
	seen := make(map[int]struct{}, len(files))
	for _, file := range files {
		if _, duplicate := seen[file.Index]; duplicate {
			return nil, 0, fmt.Errorf("duplicate qBittorrent file index %d", file.Index)
		}
		seen[file.Index] = struct{}{}
		descriptor, ok := descriptorByIndex[file.Index]
		if !ok || descriptor.RelativePath != file.Name || descriptor.SizeBytes != file.Size {
			return nil, 0, fmt.Errorf("qBittorrent file %d does not match metainfo", file.Index)
		}
		_, materialized := materializedPaths[file.Name]
		status := models.CrossSeedPartialPoolFileStatusMissing
		if materialized {
			status = models.CrossSeedPartialPoolFileStatusPresent
		} else if file.Priority > 0 {
			missingBytes += file.Size
		}
		rows = append(rows, models.CrossSeedPartialPoolMemberFile{
			FileIndex:         file.Index,
			RelativePath:      descriptor.RelativePath,
			SizeBytes:         descriptor.SizeBytes,
			PiecesRoot:        descriptor.PiecesRoot,
			WantedAtAdmission: file.Priority > 0,
			MaterializedAtAdd: materialized,
			Status:            status,
		})
	}
	return rows, missingBytes, nil
}

// partialPoolReplaceableTargets records missing paths immediately before the
// qBittorrent add so only placeholders created after that point may be replaced.
func partialPoolReplaceableTargets(rootPath string, descriptors []partialPoolFileDescriptor) map[string]struct{} {
	replaceable := make(map[string]struct{})
	member := &models.CrossSeedPartialPoolMember{RootPath: rootPath}
	for _, descriptor := range descriptors {
		file := &models.CrossSeedPartialPoolMemberFile{RelativePath: descriptor.RelativePath}
		targetPath, err := partialPoolLocalPath(member, file)
		if err != nil {
			continue
		}
		if _, err := os.Lstat(targetPath); os.IsNotExist(err) {
			replaceable[descriptor.RelativePath] = struct{}{}
		}
	}
	return replaceable
}

func partialPoolCommonRoot(files []*models.CrossSeedPartialPoolMemberFile) string {
	var root string
	for _, file := range files {
		if file == nil {
			continue
		}
		parts := strings.SplitN(file.RelativePath, "/", 2)
		if len(parts) != 2 || parts[0] == "" {
			return ""
		}
		if root == "" {
			root = parts[0]
		} else if root != parts[0] {
			return ""
		}
	}
	return root
}

func partialPoolRootStrippedPath(member *models.CrossSeedPartialPoolMember, file *models.CrossSeedPartialPoolMemberFile) string {
	if member == nil || file == nil {
		return ""
	}
	root := partialPoolCommonRoot(member.Files)
	if root == "" {
		return file.RelativePath
	}
	return strings.TrimPrefix(file.RelativePath, root+"/")
}

func partialPoolFilesPair(sourceMember, targetMember *models.CrossSeedPartialPoolMember, source, target *models.CrossSeedPartialPoolMemberFile) bool {
	if source == nil || target == nil || source.SizeBytes <= 0 || source.SizeBytes != target.SizeBytes {
		return false
	}
	if source.PiecesRoot != "" && target.PiecesRoot != "" {
		return strings.EqualFold(source.PiecesRoot, target.PiecesRoot)
	}
	if strings.EqualFold(partialPoolRootStrippedPath(sourceMember, source), partialPoolRootStrippedPath(targetMember, target)) {
		return true
	}
	sourceBase := strings.ToLower(path.Base(source.RelativePath))
	targetBase := strings.ToLower(path.Base(target.RelativePath))
	if sourceBase == "" || sourceBase != targetBase {
		return false
	}
	return partialPoolBasenameSizeCount(sourceMember, sourceBase, source.SizeBytes) == 1 &&
		partialPoolBasenameSizeCount(targetMember, targetBase, target.SizeBytes) == 1
}

func partialPoolBasenameSizeCount(member *models.CrossSeedPartialPoolMember, basename string, size int64) int {
	if member == nil {
		return 0
	}
	count := 0
	for _, file := range member.Files {
		if file != nil && file.SizeBytes == size && strings.EqualFold(path.Base(file.RelativePath), basename) {
			count++
		}
	}
	return count
}

func partialPoolLocalPath(member *models.CrossSeedPartialPoolMember, file *models.CrossSeedPartialPoolMemberFile) (string, error) {
	if member == nil || file == nil {
		return "", errors.New("partial pool member and file are required")
	}
	plan, err := hardlinktree.BuildSingleFilePlan(member.RootPath, file.RelativePath, member.RootPath)
	if err != nil {
		return "", err
	}
	target := plan.Files[0].TargetPath
	if err := validatePartialPoolPathInsideRoot(member.RootPath, target); err != nil {
		return "", err
	}
	return target, nil
}

// validatePartialPoolPathInsideRoot rejects lexical and symlink/reparse-point
// escapes for existing and not-yet-created targets.
func validatePartialPoolPathInsideRoot(root, target string) error {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	if err := pathInside(rootAbs, targetAbs); err != nil {
		return err
	}

	resolvedRoot, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("resolve partial pool root: %w", err)
		}
		resolvedRoot = rootAbs
	}
	resolvedTarget, err := filepath.EvalSymlinks(targetAbs)
	if err == nil {
		if err := pathInside(resolvedRoot, resolvedTarget); err != nil {
			return fmt.Errorf("partial pool path escapes through symlink or reparse point: %w", err)
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return fmt.Errorf("resolve partial pool target: %w", err)
	}
	existing := filepath.Dir(targetAbs)
	var suffix []string
	for {
		_, statErr := os.Lstat(existing)
		if statErr == nil {
			break
		}
		if !os.IsNotExist(statErr) {
			return statErr
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return statErr
		}
		suffix = append(suffix, filepath.Base(existing))
		existing = parent
	}
	resolvedExisting, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return fmt.Errorf("resolve partial pool path: %w", err)
	}
	for _, s := range slices.Backward(suffix) {
		resolvedExisting = filepath.Join(resolvedExisting, s)
	}
	resolvedTarget = filepath.Join(resolvedExisting, filepath.Base(targetAbs))
	if err := pathInside(resolvedRoot, resolvedTarget); err != nil {
		return fmt.Errorf("partial pool path escapes through symlink or reparse point: %w", err)
	}
	return nil
}

func pathInside(root, target string) error {
	relative, err := filepath.Rel(root, target)
	if err != nil {
		return err
	}
	if relative == ".." || filepath.IsAbs(relative) || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path %q escapes root %q", target, root)
	}
	return nil
}

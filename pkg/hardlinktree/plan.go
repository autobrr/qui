// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package hardlinktree

import (
	"cmp"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/autobrr/qui/pkg/pathutil"
)

type TorrentFile struct {
	Path string
	Size int64
}

type ExistingFile struct {
	AbsPath string
	RelPath string
	Size    int64
}

type FilePlan struct {
	SourcePath string
	TargetPath string
}

type TreePlan struct {
	RootDir string
	Files   []FilePlan
}

type LinkPlanErrorKind string

const (
	LinkPlanErrorNoMatchingFile  LinkPlanErrorKind = "no_matching_file"
	LinkPlanErrorNoAvailableFile LinkPlanErrorKind = "no_available_file"
	LinkPlanErrorCouldNotMatch   LinkPlanErrorKind = "could_not_match"
)

type LinkPlanError struct {
	Kind LinkPlanErrorKind
	File string
}

func (e *LinkPlanError) Error() string {
	if e == nil {
		return ""
	}

	switch e.Kind {
	case LinkPlanErrorNoMatchingFile:
		return "no matching file for: " + e.File
	case LinkPlanErrorNoAvailableFile:
		return "no available match for: " + e.File
	case LinkPlanErrorCouldNotMatch:
		return "could not match file: " + e.File
	default:
		return "link plan error"
	}
}

func (e *LinkPlanError) Is(target error) bool {
	t, ok := target.(*LinkPlanError)
	return ok && e != nil && e.Kind == t.Kind
}

var (
	ErrNoMatchingFile  = &LinkPlanError{Kind: LinkPlanErrorNoMatchingFile}
	ErrNoAvailableFile = &LinkPlanError{Kind: LinkPlanErrorNoAvailableFile}
	ErrCouldNotMatch   = &LinkPlanError{Kind: LinkPlanErrorCouldNotMatch}

	ErrUnstorableName = errors.New("destination cannot store torrent path")
)

// PathDialect is the path grammar of the filesystem a plan targets.
// fsops.PathDialect satisfies it; fsops imports this package, so it is declared here.
type PathDialect interface {
	Join(base, name string) string
	Rel(base, target string) (string, error)
	FromSlash(p string) string
	Separator() string
}

type ContentLayout string

const (
	LayoutOriginal    ContentLayout = "Original"
	LayoutSubfolder   ContentLayout = "Subfolder"
	LayoutNoSubfolder ContentLayout = "NoSubfolder"
)

// BuildPlan creates a hardlink tree plan that maps existing files to the incoming torrent's layout.
//
// Parameters:
//   - d: Path grammar of the filesystem destDir and the existing files live on
//   - candidateFiles: Files as they appear in the INCOMING torrent (target layout)
//   - existingFiles: Files from the MATCHED torrent that already exist on disk
//   - layout: How qBittorrent will organize the torrent (from instance preferences)
//   - torrentName: Name of the incoming torrent (used for Subfolder layout)
//   - destDir: Base directory for the hardlink tree
//
// The resulting tree will match what qBittorrent expects when adding the torrent.
func BuildPlan(
	d PathDialect,
	candidateFiles []TorrentFile,
	existingFiles []ExistingFile,
	layout ContentLayout,
	torrentName string,
	destDir string,
) (*TreePlan, error) {
	if len(candidateFiles) == 0 {
		return nil, errors.New("no candidate files provided")
	}
	if len(existingFiles) == 0 {
		return nil, errors.New("no existing files provided")
	}
	if destDir == "" {
		return nil, errors.New("destination directory is required")
	}

	for _, cf := range candidateFiles {
		if err := validateCandidatePath(d, cf.Path); err != nil {
			return nil, err
		}
	}

	matched, err := MatchFiles(candidateFiles, existingFiles)
	if err != nil {
		return nil, err
	}

	plan := &TreePlan{
		RootDir: destDir,
		Files:   make([]FilePlan, 0, len(matched)),
	}
	for _, m := range matched {
		targetPath := computeTargetPath(d, m.RelPath, layout, torrentName, destDir)

		if err := validateTargetInsideBase(d, targetPath, destDir); err != nil {
			return nil, err
		}

		plan.Files = append(plan.Files, FilePlan{
			SourcePath: m.AbsPath,
			TargetPath: targetPath,
		})
	}

	sort.Slice(plan.Files, func(i, j int) bool {
		return plan.Files[i].TargetPath < plan.Files[j].TargetPath
	})

	return plan, nil
}

// MatchFiles finds the existing file that each candidate file links to. A
// match has the size of the candidate and, in this order, its path, its base
// name, its name without punctuation, or is the only file of that size left.
// Each result holds the AbsPath of the existing file and the path of the
// candidate as RelPath, in the order of candidateFiles. It does no I/O.
func MatchFiles(candidateFiles []TorrentFile, existingFiles []ExistingFile) ([]ExistingFile, error) {
	existingBySize := make(map[int64][]*existingEntry)
	for i := range existingFiles {
		ef := &existingFiles[i]
		entry := &existingEntry{
			file:       ef,
			base:       strings.ToLower(fileBaseName(ef.RelPath)),
			normalized: normalizeFileKey(ef.RelPath),
		}
		existingBySize[ef.Size] = append(existingBySize[ef.Size], entry)
	}

	matched := make([]ExistingFile, 0, len(candidateFiles))
	for _, cf := range candidateFiles {
		bucket := existingBySize[cf.Size]
		if len(bucket) == 0 {
			return nil, &LinkPlanError{Kind: LinkPlanErrorNoMatchingFile, File: cf.Path}
		}

		candidateBase := strings.ToLower(fileBaseName(cf.Path))
		candidateNorm := normalizeFileKey(cf.Path)

		var available []*existingEntry
		for _, entry := range bucket {
			if !entry.used {
				available = append(available, entry)
			}
		}

		if len(available) == 0 {
			return nil, &LinkPlanError{Kind: LinkPlanErrorNoAvailableFile, File: cf.Path}
		}

		var match *existingEntry

		for _, entry := range available {
			if entry.file.RelPath == cf.Path {
				match = entry
				break
			}
		}

		if match == nil {
			var candidates []*existingEntry
			for _, entry := range available {
				if entry.base == candidateBase {
					candidates = append(candidates, entry)
				}
			}
			if len(candidates) == 1 {
				match = candidates[0]
			}
		}

		if match == nil {
			var candidates []*existingEntry
			for _, entry := range available {
				if entry.normalized == candidateNorm {
					candidates = append(candidates, entry)
				}
			}
			if len(candidates) == 1 {
				match = candidates[0]
			}
		}

		if match == nil && len(available) == 1 {
			match = available[0]
		}

		if match == nil {
			return nil, &LinkPlanError{Kind: LinkPlanErrorCouldNotMatch, File: cf.Path}
		}

		match.used = true
		matched = append(matched, ExistingFile{AbsPath: match.file.AbsPath, RelPath: cf.Path, Size: cf.Size})
	}
	return matched, nil
}

func BuildSingleFilePlan(d PathDialect, rootDir, relativePath, sourcePath string) (*TreePlan, error) {
	if rootDir == "" {
		return nil, errors.New("destination directory is required")
	}
	if sourcePath == "" {
		return nil, errors.New("source path is required")
	}
	if err := ValidateRelativePath(d, relativePath); err != nil {
		return nil, err
	}

	targetPath := d.Join(rootDir, d.FromSlash(relativePath))
	if err := validateTargetInsideBase(d, targetPath, rootDir); err != nil {
		return nil, err
	}
	return &TreePlan{
		RootDir: rootDir,
		Files: []FilePlan{{
			SourcePath: sourcePath,
			TargetPath: targetPath,
		}},
	}, nil
}

func BuildLinkPlan(d PathDialect, rootDir string, files []ExistingFile) (*TreePlan, error) {
	if rootDir == "" {
		return nil, errors.New("destination directory is required")
	}
	if len(files) == 0 {
		return nil, errors.New("no files to link")
	}
	plan := &TreePlan{RootDir: rootDir, Files: make([]FilePlan, 0, len(files))}
	for _, f := range files {
		if f.AbsPath == "" {
			return nil, errors.New("source path is required for: " + f.RelPath)
		}
		if err := validateCandidatePath(d, f.RelPath); err != nil {
			return nil, err
		}
		targetPath := computeTargetPath(d, f.RelPath, LayoutOriginal, "", rootDir)
		if err := validateTargetInsideBase(d, targetPath, rootDir); err != nil {
			return nil, err
		}
		plan.Files = append(plan.Files, FilePlan{SourcePath: f.AbsPath, TargetPath: targetPath})
	}
	slices.SortFunc(plan.Files, func(a, b FilePlan) int {
		return cmp.Compare(a.TargetPath, b.TargetPath)
	})
	return plan, nil
}

func ValidateTorrentPath(d PathDialect, name string) error {
	return validateCandidatePath(d, name)
}

type existingEntry struct {
	file       *ExistingFile
	base       string
	normalized string
	used       bool
}

// computeTargetPath determines the full target path based on content layout.
// candidatePath is a validated slash path; it is converted only where it is joined.
func computeTargetPath(d PathDialect, candidatePath string, layout ContentLayout, torrentName, destDir string) string {
	switch layout {
	case LayoutSubfolder:
		// Wrap in a folder named after the torrent
		// For single-file torrents, qBittorrent strips the extension from folder name
		folderName := torrentName
		if !strings.Contains(candidatePath, "/") {
			if ext := filepath.Ext(torrentName); ext != "" {
				folderName = strings.TrimSuffix(torrentName, ext)
			}
		}
		// Sanitize folder name for Windows compatibility (removes illegal characters)
		// but keep candidatePath unchanged to match incoming torrent layout
		folderName = pathutil.SanitizePathSegment(folderName)
		return d.Join(d.Join(destDir, folderName), d.FromSlash(candidatePath))

	case LayoutNoSubfolder:
		parts := strings.SplitN(candidatePath, "/", 2)
		if len(parts) == 2 {
			return d.Join(destDir, d.FromSlash(parts[1]))
		}
		return d.Join(destDir, d.FromSlash(candidatePath))

	default:
		return d.Join(destDir, d.FromSlash(candidatePath))
	}
}

func ValidateRelativePath(d PathDialect, name string) error {
	if err := validateCandidatePath(d, name); err != nil {
		return err
	}
	if path.Clean(name) != name {
		return errors.New("non-canonical torrent path: " + name)
	}
	return nil
}

// validateCandidatePath checks if a torrent file path is safe (no path traversal)
// and that every component is a name the destination dialect d can store. It
// reads the name in slash grammar on every host, so the answer depends on the
// destination, not on the OS qui runs on.
func validateCandidatePath(d PathDialect, name string) error {
	if name == "" {
		return errors.New("empty file path in torrent")
	}

	// Torrent paths are slash-delimited. Reject both platform-native and
	// foreign absolute/traversal forms on every host OS.
	if strings.HasPrefix(name, "/") || strings.HasPrefix(name, `\`) {
		return errors.New("absolute path not allowed in torrent: " + name)
	}
	if strings.Contains(name, `\`) {
		return errors.New("backslash not allowed in torrent path: " + name)
	}
	if len(name) >= 2 && ((name[0] >= 'a' && name[0] <= 'z') || (name[0] >= 'A' && name[0] <= 'Z')) && name[1] == ':' {
		return errors.New("volume prefix not allowed in torrent path: " + name)
	}

	// path.Clean leaves ".." only as leading segments.
	p := path.Clean(name)
	if p == "." {
		return errors.New("current directory path not allowed: " + name)
	}
	if p == ".." || strings.HasPrefix(p, "../") {
		return errors.New("parent directory traversal not allowed: " + name)
	}

	// A backslash separator means a Windows destination, which cannot store every POSIX name.
	if d.Separator() == `\` {
		for component := range strings.SplitSeq(p, "/") {
			if err := windowsNameError(component); err != nil {
				return fmt.Errorf("%w %q: %w", ErrUnstorableName, name, err)
			}
		}
	}

	return nil
}

// windowsNameError reports why Windows cannot store component as a file name.
// NTFS reads "name:stream" as an alternate data stream of "name", so a colon
// would silently address another file.
func windowsNameError(component string) error {
	if i := strings.IndexFunc(component, func(r rune) bool { return r < 0x20 || strings.ContainsRune(`<>:"|?*`, r) }); i >= 0 {
		return fmt.Errorf("character %q not allowed in Windows file name %q", component[i], component)
	}
	if strings.HasSuffix(component, ".") || strings.HasSuffix(component, " ") {
		return fmt.Errorf("file name %q ends in a dot or space, which Windows cannot store", component)
	}
	if pathutil.IsWindowsDeviceName(component) {
		return fmt.Errorf("file name %q is a reserved Windows device name", component)
	}
	return nil
}

func validateTargetInsideBase(d PathDialect, targetPath, baseDir string) error {
	rel, err := d.Rel(baseDir, targetPath)
	if err != nil {
		return errors.New("failed to compute relative path: " + err.Error())
	}

	if strings.HasPrefix(rel, ".."+d.Separator()) || rel == ".." {
		return errors.New("target path escapes base directory: " + targetPath)
	}

	return nil
}

func fileBaseName(path string) string {
	path = filepath.FromSlash(path)
	return filepath.Base(path)
}

func normalizeFileKey(path string) string {
	base := fileBaseName(path)
	if base == "" {
		return ""
	}

	ext := ""
	if dot := strings.LastIndex(base, "."); dot >= 0 && dot < len(base)-1 {
		ext = strings.ToLower(base[dot+1:])
		base = base[:dot]
	}

	sidecarExts := map[string]bool{
		"nfo": true, "srt": true, "sub": true, "idx": true, "sfv": true, "txt": true,
	}
	if sidecarExts[ext] {
		if dot := strings.LastIndex(base, "."); dot >= 0 && dot < len(base)-1 {
			videoExt := strings.ToLower(base[dot+1:])
			videoExts := map[string]bool{
				"mkv": true, "mp4": true, "avi": true, "ts": true,
				"m2ts": true, "mov": true, "mpg": true, "mpeg": true,
			}
			if videoExts[videoExt] {
				base = base[:dot]
			}
		}
	}

	var sb strings.Builder
	for _, r := range strings.ToLower(base) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			sb.WriteRune(r)
		}
	}

	if ext != "" {
		sb.WriteRune('.')
		sb.WriteString(ext)
	}

	return sb.String()
}

func HasCommonRootFolder(files []TorrentFile) bool {
	if len(files) == 0 {
		return false
	}

	var commonRoot string
	for i, f := range files {
		path := filepath.FromSlash(f.Path)
		parts := strings.SplitN(path, string(filepath.Separator), 2)

		if len(parts) < 2 {
			return false
		}

		root := parts[0]
		if i == 0 {
			commonRoot = root
		} else if root != commonRoot {
			return false
		}
	}

	return commonRoot != ""
}

package clientmigrate

import (
	"archive/tar"
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/klauspost/compress/gzip"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
)

type ClientType string

var (
	ClientTypeDeluge       ClientType = "deluge"
	ClientTypeRTorrent     ClientType = "rtorrent"
	ClientTypeTransmission ClientType = "transmission"
	ClientTypeUTorrent     ClientType = "utorrent"
)

type Options struct {
	Source       ClientType
	SourceDir    string
	QbitDir      string
	DryRun       bool
	SkipBackup   bool
	PathReplaces []PathReplace
}

// PathReplace rewrites save paths that start with From to start with To
type PathReplace struct {
	From string
	To   string
}

// ParsePathReplace parses a from=to rule. To must be an absolute path on
// this host; From is a path as the source client stored it
func ParsePathReplace(rule string) (PathReplace, error) {
	from, to, ok := strings.Cut(rule, "=")
	if !ok || from == "" || to == "" {
		return PathReplace{}, errors.Errorf("invalid replace path %q, expected from=to", rule)
	}

	if !filepath.IsAbs(to) {
		return PathReplace{}, errors.Errorf("invalid replace path %q, %s is not an absolute path on this OS", rule, to)
	}

	// only / may match every path, \\ or // would rewrite them all
	if from != "/" && strings.Trim(from, `/\`) == "" {
		return PathReplace{}, errors.Errorf("invalid replace path %q, %s is not a usable source path", rule, from)
	}

	return PathReplace{From: from, To: to}, nil
}

type ClientMigrater interface {
	Migrate() error
}

type Migrater struct {
	imp  ClientMigrater
	opts Options
}

func New(opts Options) Migrater {
	m := Migrater{opts: opts}

	switch m.opts.Source {
	case ClientTypeDeluge:
		m.imp = NewDelugeImporter(m.opts)
	case ClientTypeRTorrent:
		m.imp = NewRTorrentImporter(m.opts)
	case ClientTypeTransmission:
		m.imp = NewTransmissionImporter(m.opts)
	case ClientTypeUTorrent:
		m.imp = NewUTorrentImporter(m.opts)
	default:
		log.Fatal().Str("source", string(m.opts.Source)).Msg("unsupported source client")
	}

	return m
}

func (m Migrater) Migrate(ctx context.Context) error {
	var (
		dryRun    = m.opts.DryRun
		source    = m.opts.Source
		sourceDir = m.opts.SourceDir
	)

	// Backup data before running
	if !m.opts.SkipBackup {
		if err := m.Backup(); err != nil {
			log.Error().Err(err).Msgf("Could not backup files")
			return err
		}
	}

	start := time.Now()

	if dryRun {
		log.Info().Msgf("dry-run: preparing to import torrents from: %s dir: %s", source, sourceDir)
		log.Info().Msg("dry-run: no data will be written")
	} else {
		log.Info().Msgf("preparing to import torrents from: %s dir: %s", source, sourceDir)
	}

	if err := m.imp.Migrate(); err != nil {
		return errors.Wrapf(err, "could not import from %s", source)
	}

	elapsed := time.Since(start)

	log.Info().Msgf("Import finished in: %s", elapsed)

	return nil
}

func (m Migrater) Backup() error {
	log.Info().Msg("prepare to backup torrent data before import..")

	var (
		source    = m.opts.Source
		sourceDir = m.opts.SourceDir
		qbitDir   = m.opts.QbitDir
	)

	timeStamp := time.Now().Format("20060102150405")

	backupDir := "qbt_backup"

	sourceBackupArchive := filepath.Join(backupDir, string(source)+"_backup_"+timeStamp+".tar.gz")
	qbitBackupArchive := filepath.Join(backupDir, "qBittorrent_backup_"+timeStamp+".tar.gz")

	if m.opts.DryRun {
		log.Info().Msgf("dry-run: creating %s backup of directory: %s to %s ...", source, sourceDir, sourceBackupArchive)
		log.Info().Msgf("dry-run: creating qBittorrent backup of directory: %s to %s ...", qbitDir, qbitBackupArchive)
	} else {
		if err := os.MkdirAll(backupDir, os.ModePerm); err != nil {
			return errors.Wrap(err, "could not create backup directory")
		}

		log.Info().Msgf("creating %s backup of directory: %s to %s ...", source, sourceDir, sourceBackupArchive)

		if err := archiveDir(sourceDir, sourceBackupArchive); err != nil {
			return errors.Wrapf(err, "could not create %s backup of directory: %s to %s", source, sourceDir, sourceBackupArchive)
		}

		// a fresh qBittorrent install may not have a BT_backup dir yet
		if _, err := os.Stat(qbitDir); os.IsNotExist(err) {
			log.Info().Msgf("qBittorrent directory does not exist yet, skipping backup: %s", qbitDir)
		} else {
			log.Info().Msgf("creating qBittorrent backup of directory: %s to %s ...", qbitDir, qbitBackupArchive)

			if err := archiveDir(qbitDir, qbitBackupArchive); err != nil {
				return errors.Wrapf(err, "could not create qBittorrent backup of directory: %s", qbitDir)
			}
		}
	}

	log.Info().Msg("Backup completed!")

	return nil
}

// archiveDir writes every regular file under dir into a tar.gz archive with
// paths relative to dir
func archiveDir(dir, archiveName string) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()

	// with --source-dir . the archive lands inside the walked tree; it must
	// not be written into itself
	archiveAbs, err := filepath.Abs(archiveName)
	if err != nil {
		return err
	}

	out, err := os.Create(archiveName)
	if err != nil {
		return err
	}

	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)

	walkErr := fs.WalkDir(root.FS(), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		if abs, err := filepath.Abs(filepath.Join(dir, filepath.FromSlash(path))); err == nil && abs == archiveAbs {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}

		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = path

		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}

		f, err := root.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()

		_, err = io.Copy(tw, f)
		return err
	})
	if walkErr != nil {
		out.Close()
		return errors.Wrapf(walkErr, "could not create backup archive: %s", archiveName)
	}

	if err := tw.Close(); err != nil {
		out.Close()
		return err
	}
	if err := gz.Close(); err != nil {
		out.Close()
		return err
	}

	return out.Close()
}

// replacePath rewrites the prefix of a source client path with the first
// matching rule and returns it in host form, or returns p unchanged. Matches
// end at a path separator, so /data does not match /database. Windows-style
// rules (drive letters and UNC shares) match case-insensitively with either
// slash, which lets a library move between Windows and Unix hosts
func replacePath(p string, rules []PathReplace) string {
	for _, r := range rules {
		// convert slashes by the rule's style, so a POSIX rule keeps
		// literal backslashes and does not match a \\nas\share path
		posix := strings.HasPrefix(r.From, "/") && !strings.HasPrefix(r.From, "//")
		src, from := p, r.From
		if !posix {
			src, from = strings.ReplaceAll(p, `\`, "/"), strings.ReplaceAll(r.From, `\`, "/")
		}
		from = strings.TrimRight(from, "/")
		if len(src) < len(from) {
			continue
		}

		prefix, rest := src[:len(from)], src[len(from):]
		if rest != "" && rest[0] != '/' {
			continue
		}
		if prefix != from && (posix || !strings.EqualFold(prefix, from)) {
			continue
		}

		return filepath.Join(r.To, filepath.FromSlash(rest))
	}

	return p
}

// foreignAbs reports whether p is absolute on another OS but not on this
// host: a Windows drive or UNC path on Unix, a POSIX path on Windows. libtorrent
// would resolve such a path against the save path
func foreignAbs(p string) bool {
	if filepath.IsAbs(p) {
		return false
	}

	// a single leading backslash is a valid POSIX file name character
	if strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\\`) {
		return true
	}

	return len(p) >= 3 && p[1] == ':' && (p[2] == '/' || p[2] == '\\') && ('a' <= p[0]|0x20 && p[0]|0x20 <= 'z')
}

// sourceSlash turns the backslashes of a Windows-style path into slashes.
// POSIX paths keep theirs, a backslash is a valid file name character there;
// a leading // is a UNC share written with forward slashes
func sourceSlash(p string) string {
	if strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "//") {
		return p
	}
	return strings.ReplaceAll(p, `\`, "/")
}

// logImportSummary logs the per-importer end result with accurate counts
func logImportSummary(dryRun bool, imported, failed, skipped, total int) {
	switch {
	case dryRun:
		log.Info().Msgf("dry-run: would import %d of %d torrents, %d failed, %d skipped", imported, total, failed, skipped)
	case failed > 0 || skipped > 0:
		log.Warn().Msgf("imported %d of %d torrents, %d failed, %d skipped", imported, total, failed, skipped)
	default:
		log.Info().Msgf("successfully imported %d torrents!", imported)
	}
}

// wantedPieces reports, for each piece, whether any wanted file overlaps it,
// given the file lengths in torrent order and per-file wanted flags
func wantedPieces(fileLengths []int64, wanted []bool, pieceLen int64, numPieces int) []bool {
	wp := make([]bool, numPieces)

	var offset int64
	for i, length := range fileLengths {
		if wanted[i] && length > 0 {
			start := int(offset / pieceLen)
			end := int((offset + length - 1) / pieceLen)
			for p := start; p <= end && p < numPieces; p++ {
				wp[p] = true
			}
		}
		offset += length
	}

	return wp
}

// firstNonZero returns the first non-zero value
func firstNonZero(values ...int64) int64 {
	for _, v := range values {
		if v != 0 {
			return v
		}
	}

	return 0
}

// CopyFile copies the file named src to dst with the mode of src. It writes
// a temp file next to dst and renames it into place after the data is synced,
// so a failed copy never leaves a partial dst: the importers treat an existing
// dst as already imported and would skip it on the next run
func CopyFile(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	si, err := in.Stat()
	if err != nil {
		return err
	}

	out, err := os.CreateTemp(filepath.Dir(dst), filepath.Base(dst)+".tmp-*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			out.Close()
			os.Remove(out.Name())
		}
	}()

	if _, err = io.Copy(out, in); err != nil {
		return err
	}

	if err = out.Sync(); err != nil {
		return err
	}

	if err = out.Chmod(si.Mode()); err != nil {
		return err
	}

	if err = out.Close(); err != nil {
		return err
	}

	return os.Rename(out.Name(), dst)
}

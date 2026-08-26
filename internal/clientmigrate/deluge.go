package clientmigrate

import (
	"bytes"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/autobrr/go-torrent/bencode"
	"github.com/autobrr/go-torrent/metainfo"

	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
)

type DelugeImport struct {
	opts Options
}

func NewDelugeImporter(opts Options) *DelugeImport {
	return &DelugeImport{opts: opts}
}

func (di *DelugeImport) Migrate() error {
	sourceDir := di.opts.SourceDir

	sourceDirInfo, err := os.Stat(sourceDir)
	if err != nil {
		if os.IsNotExist(err) {
			return errors.Errorf("source directory does not exist: %s", sourceDir)
		}

		return errors.Wrapf(err, "source directory error: %s", sourceDir)
	}

	if !sourceDirInfo.IsDir() {
		return errors.Errorf("source is a file, not a directory: %s", sourceDir)
	}

	if !di.opts.DryRun {
		if err := os.MkdirAll(di.opts.QbitDir, os.ModePerm); err != nil {
			return errors.Wrapf(err, "qbit directory error: %s", di.opts.QbitDir)
		}
	}

	// deluge itself falls back to the .bak copy and the pre-1.3 location,
	// including when the main file exists but is corrupt
	var fastresumeFile map[string]any
	for _, candidate := range []string{
		filepath.Join(sourceDir, "torrents.fastresume"),
		filepath.Join(sourceDir, "torrents.fastresume.bak"),
		filepath.Join(sourceDir, "..", "torrents.fastresume"),
	} {
		if _, err := os.Stat(candidate); err != nil {
			continue
		}

		decoded, err := decodeFastresumeFile(candidate)
		if err != nil {
			log.Warn().Err(err).Msgf("Could not decode deluge fastresume file, trying next candidate: %s", candidate)
			continue
		}

		fastresumeFile = decoded
		break
	}
	if fastresumeFile == nil {
		return errors.Errorf("could not find a readable deluge fastresume file in: %s", sourceDir)
	}

	labels := readDelugeLabels(sourceDir)

	totalJobs := len(fastresumeFile)

	log.Info().Msgf("Total torrents to process: %d", totalJobs)

	positionNum := 0
	imported := 0
	failed := 0
	skipped := 0
	for _, torrentID := range slices.Sorted(maps.Keys(fastresumeFile)) {
		value := fastresumeFile[torrentID]

		torrentNamePath := filepath.Join(sourceDir, torrentID+".torrent")

		// If a file exist in fastresume data but no .torrent file, skip
		if _, err = os.Stat(torrentNamePath); os.IsNotExist(err) {
			log.Error().Err(err).Msgf("%s: skipping because %s not found in source directory", torrentID, torrentNamePath)
			failed++
			continue
		}

		positionNum++

		torrentOutFile := filepath.Join(di.opts.QbitDir, torrentID+".torrent")

		// If file already exists, skip
		if _, err = os.Stat(torrentOutFile); err == nil {
			log.Info().Msgf("(%d/%d) %s Torrent already exists, skipping", positionNum, totalJobs, torrentID)
			skipped++
			continue
		}

		var fastResume Fastresume

		strValue, ok := value.(string)
		if !ok {
			log.Error().Msgf("Could not convert value %v to string", value)
			failed++
			continue
		}

		if err := bencode.NewDecoder(strings.NewReader(strValue)).Decode(&fastResume); err != nil {
			log.Error().Err(err).Msgf("Could not decode row %s. Continue", torrentID)
			failed++
			continue
		}

		fastResume.TorrentFilePath = torrentNamePath

		file, err := metainfo.LoadFromFile(torrentNamePath)
		if err != nil {
			log.Error().Err(err).Msgf("Could not load torrent file %s for %s", fastResume.TorrentFilePath, torrentID)
			failed++
			continue
		}

		metaInfo, err := file.UnmarshalInfo()
		if err != nil {
			log.Error().Err(err).Msgf("Could not unmarshal torrent file %s for %s", fastResume.TorrentFilePath, torrentID)
			failed++
			continue
		}

		// v2-only torrents lose their merkle trees in this format;
		// qBittorrent would reject or mis-verify them
		if metaInfo.MetaVersion == 2 {
			log.Warn().Msgf("(%d/%d) %s is a BitTorrent v2 torrent, not supported by this importer, skipping", positionNum, totalJobs, torrentID)
			skipped++
			continue
		}

		// qBittorrent rejects resume data without an exact 20-byte v1 infohash
		if len(fastResume.InfoHash) != 20 {
			log.Warn().Msgf("(%d/%d) %s resume data has no valid v1 infohash, skipping", positionNum, totalJobs, torrentID)
			skipped++
			continue
		}

		// resume data that does not belong to this torrent file would import
		// piece state for different content
		if !bytes.Equal(fastResume.InfoHash, file.HashInfoBytes().Bytes()) {
			log.Warn().Msgf("(%d/%d) %s resume data does not match the torrent file, skipping", positionNum, totalJobs, torrentID)
			skipped++
			continue
		}

		// a corrupt entry with an unusable save path would import a torrent
		// qBittorrent resolves against its own default download dir
		if !filepath.IsAbs(filepath.Clean(fastResume.SavePath)) {
			log.Warn().Msgf("(%d/%d) %s has an unusable save path %q, skipping", positionNum, totalJobs, torrentID, fastResume.SavePath)
			skipped++
			continue
		}

		// complete means every piece of every wanted file has its have-bit
		// set; files with priority 0 may legitimately be missing
		pieces, complete := delugePieces(&fastResume, &metaInfo)
		if !complete {
			log.Warn().Msgf("(%d/%d) %s is not fully downloaded, skipping: %s", positionNum, totalJobs, metaInfo.BestName(), torrentID)
			skipped++
			continue
		}

		// valid QbtContentLayout = Original, Subfolder, NoSubfolder
		fastResume.QbtContentLayout = "Original"
		if metaInfo.IsDir() {
			// legacy and should be removed sometime with 4.3.X
			fastResume.QbtHasRootFolder = 1
		} else {
			fastResume.QbtHasRootFolder = 0
		}

		fastResume.QbtRatioLimit = -2000
		fastResume.QbtSeedStatus = 1
		fastResume.QbtSeedingTimeLimit = -2
		fastResume.QbtCategory = labels[torrentID]
		fastResume.QbtTags = []string{"migrated"}
		// deluge stores renames in mapped_files and leaves name at the
		// metainfo value; derive the display name from the first mapped path
		if len(fastResume.MappedFiles) > 0 {
			renamed := fastResume.MappedFiles[0]
			if metaInfo.IsDir() {
				if i := strings.IndexByte(renamed, '/'); i > 0 {
					renamed = renamed[:i]
				}
			}
			if renamed != "" && renamed != metaInfo.BestName() {
				fastResume.QbtName = renamed
			}
		}
		fastResume.QbtSavePath = fastResume.SavePath
		fastResume.QbtQueuePosition = positionNum

		// deluge 1.3.x era resume data predates apply_ip_filter; leaving the
		// decoded zero would explicitly disable the IP filter in qBittorrent
		fastResume.ApplyIPFilter = 1
		fastResume.NumIncomplete = 0

		// libtorrent 1.0-era resume data (deluge 1.3.x) folds the session-wide
		// shutdown pause into every torrent's paused flag, so it reads 1 for
		// the whole library and the real state only exists in the
		// torrents.state pickle; resume everything for those sources, and for
		// data too old or stripped to carry a version at all. Newer libtorrent
		// records accurate per-torrent flags, keep them.
		ltVersion := fastResume.LibTorrentVersion
		if ltVersion == "" || strings.HasPrefix(ltVersion, "0.") || strings.HasPrefix(ltVersion, "1.0") {
			fastResume.Paused = 0
			fastResume.AutoManaged = 1
		}

		// libtorrent writes 4 for default-priority files; qBittorrent's enum
		// is 0 skip, 1 normal, 6 high, 7 maximum
		for idx, p := range fastResume.FilePriority {
			if p != 0 && p != 6 && p != 7 {
				fastResume.FilePriority[idx] = 1
			}
		}

		// keep the decoded libtorrent file priorities when the resume data has
		// them, they carry the user's deselected files
		if len(fastResume.FilePriority) != len(metaInfo.UpvertedFiles()) {
			fastResume.ConvertFilePriority(len(metaInfo.UpvertedFiles()))
		}

		// pieces overlapping only deselected files stay unset; drop the
		// source's partial-piece list so it cannot contradict the bitfield
		fastResume.Pieces = pieces
		fastResume.Unfinished = nil

		// TODO handle replace paths

		if di.opts.DryRun {
			log.Info().Msgf("dry-run: (%d/%d) would import: %s", positionNum, totalJobs, torrentID)
			imported++
			continue
		}

		fastResumeOutFile := filepath.Join(di.opts.QbitDir, torrentID+".fastresume")
		if err = fastResume.Encode(fastResumeOutFile); err != nil {
			log.Error().Err(err).Msgf("Could not create qBittorrent fastresume file %s", fastResumeOutFile)
			failed++
			continue
		}

		if err = CopyFile(fastResume.TorrentFilePath, torrentOutFile); err != nil {
			log.Error().Err(err).Msgf("Could not copy qBittorrent torrent file %s", torrentOutFile)
			failed++
			continue
		}

		imported++

		log.Info().Msgf("(%d/%d) successfully imported: %s %s", positionNum, totalJobs, torrentID, metaInfo.Name)
	}

	logImportSummary(di.opts.DryRun, imported, failed, skipped, totalJobs)

	return nil
}

// delugePieces converts the decoded libtorrent resume pieces (one byte per
// piece, bit 0 = have) into a qBittorrent piece bitfield and reports whether
// every piece of every wanted (non-zero priority) file is present
func delugePieces(fr *Fastresume, info *metainfo.Info) (string, bool) {
	numPieces := info.NumPieces()
	if numPieces == 0 || info.PieceLength <= 0 {
		return "", false
	}

	// a torrent that never finished its initial check has short or no pieces
	if len(fr.Pieces) < numPieces {
		return "", false
	}

	files := info.UpvertedFiles()
	wanted := make([]bool, len(files))
	for i := range wanted {
		wanted[i] = true
	}
	if len(fr.FilePriority) == len(files) {
		for i, p := range fr.FilePriority {
			wanted[i] = p != 0
		}
	}

	fileLengths := make([]int64, len(files))
	for i, f := range files {
		fileLengths[i] = f.Length
	}
	wp := wantedPieces(fileLengths, wanted, info.PieceLength, numPieces)

	pieces := make([]byte, numPieces)
	complete := true
	for p := range numPieces {
		if fr.Pieces[p]&1 == 1 {
			pieces[p] = 1
		} else if wp[p] {
			complete = false
		}
	}

	return string(pieces), complete
}

// readDelugeLabels reads the label plugin state from <config_dir>/label.conf,
// one level above the state dir. The file is two concatenated JSON documents:
// a version dict followed by the config data
func readDelugeLabels(stateDir string) map[string]string {
	path := filepath.Join(stateDir, "..", "label.conf")

	f, err := os.Open(path)
	if err != nil {
		// no label plugin state means no labels
		return nil
	}
	defer f.Close()

	dec := json.NewDecoder(f)

	var version json.RawMessage
	if err := dec.Decode(&version); err != nil {
		log.Warn().Err(err).Msgf("Could not parse deluge label config, labels will not be migrated: %s", path)
		return nil
	}

	var data struct {
		TorrentLabels map[string]string `json:"torrent_labels"`
	}
	if err := dec.Decode(&data); err != nil {
		log.Warn().Err(err).Msgf("Could not parse deluge label config, labels will not be migrated: %s", path)
		return nil
	}

	return data.TorrentLabels
}

func decodeFastresumeFile(path string) (map[string]any, error) {
	dat, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var fastresumeFile map[string]any
	if err := bencode.NewDecoder(bytes.NewReader(dat)).Decode(&fastresumeFile); err != nil {
		return nil, err
	}

	return fastresumeFile, nil
}

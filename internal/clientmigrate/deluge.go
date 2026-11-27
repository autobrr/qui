package clientmigrate

import (
	"bytes"
	"encoding/json"
	"fmt"
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

	fastresumeFile, err := di.loadFastresume()
	if err != nil {
		return err
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

		if _, err = os.Stat(torrentNamePath); os.IsNotExist(err) {
			log.Error().Err(err).Msgf("%s: skipping because %s not found in source directory", torrentID, torrentNamePath)
			failed++
			continue
		}

		positionNum++

		torrentOutFile := filepath.Join(di.opts.QbitDir, torrentID+".torrent")

		if _, err = os.Stat(torrentOutFile); err == nil {
			log.Info().Msgf("(%d/%d) %s Torrent already exists, skipping", positionNum, totalJobs, torrentID)
			skipped++
			continue
		}

		fastResume, ok := decodeDelugeEntry(value, torrentID)
		if !ok {
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

		if reason := di.checkDelugeResume(&fastResume, file, &metaInfo); reason != "" {
			log.Warn().Msgf("(%d/%d) %s %s", positionNum, totalJobs, torrentID, reason)
			skipped++
			continue
		}

		pieces, complete := delugePieces(&fastResume, &metaInfo)
		if !complete {
			log.Warn().Msgf("(%d/%d) %s is not fully downloaded, skipping: %s", positionNum, totalJobs, metaInfo.BestName(), torrentID)
			skipped++
			continue
		}

		delugeQbtFields(&fastResume, &metaInfo, labels[torrentID], positionNum, pieces)

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

func (di *DelugeImport) loadFastresume() (map[string]any, error) {
	sourceDir := di.opts.SourceDir

	sourceDirInfo, err := os.Stat(sourceDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errors.Errorf("source directory does not exist: %s", sourceDir)
		}

		return nil, errors.Wrapf(err, "source directory error: %s", sourceDir)
	}

	if !sourceDirInfo.IsDir() {
		return nil, errors.Errorf("source is a file, not a directory: %s", sourceDir)
	}

	if !di.opts.DryRun {
		if err := os.MkdirAll(di.opts.QbitDir, os.ModePerm); err != nil {
			return nil, errors.Wrapf(err, "qbit directory error: %s", di.opts.QbitDir)
		}
	}

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
		return nil, errors.Errorf("could not find a readable deluge fastresume file in: %s", sourceDir)
	}

	return fastresumeFile, nil
}

func decodeDelugeEntry(value any, torrentID string) (Fastresume, bool) {
	var fastResume Fastresume

	strValue, ok := value.(string)
	if !ok {
		log.Error().Msgf("Could not convert value %v to string", value)
		return fastResume, false
	}

	if err := bencode.NewDecoder(strings.NewReader(strValue)).Decode(&fastResume); err != nil {
		log.Error().Err(err).Msgf("Could not decode row %s. Continue", torrentID)
		return fastResume, false
	}

	return fastResume, true
}

func (di *DelugeImport) checkDelugeResume(fr *Fastresume, file *metainfo.MetaInfo, info *metainfo.Info) string {
	if info.MetaVersion == 2 {
		return "is a BitTorrent v2 torrent, not supported by this importer, skipping"
	}

	if len(fr.InfoHash) != 20 {
		return "resume data has no valid v1 infohash, skipping"
	}

	if !bytes.Equal(fr.InfoHash, file.HashInfoBytes().Bytes()) {
		return "resume data does not match the torrent file, skipping"
	}

	fr.SavePath = replacePath(fr.SavePath, di.opts.PathReplaces)
	for idx, p := range fr.MappedFiles {
		fr.MappedFiles[idx] = replacePath(p, di.opts.PathReplaces)
	}

	if !filepath.IsAbs(filepath.Clean(fr.SavePath)) {
		return fmt.Sprintf("has an unusable save path %q, skipping, map it with --replace-path", fr.SavePath)
	}

	if idx := slices.IndexFunc(fr.MappedFiles, foreignAbs); idx >= 0 {
		return fmt.Sprintf("has a moved file at unusable path %q, skipping, map it with --replace-path", fr.MappedFiles[idx])
	}

	return ""
}

func delugeQbtFields(fr *Fastresume, info *metainfo.Info, label string, positionNum int, pieces string) {
	fr.QbtContentLayout = "Original"
	if info.IsDir() {
		fr.QbtHasRootFolder = 1
	} else {
		fr.QbtHasRootFolder = 0
	}

	fr.QbtRatioLimit = -2000
	fr.QbtSeedStatus = 1
	fr.QbtSeedingTimeLimit = -2
	fr.QbtCategory = label
	fr.QbtTags = []string{"migrated"}
	if len(fr.MappedFiles) > 0 {
		renamed := fr.MappedFiles[0]
		if info.IsDir() {
			if i := strings.IndexByte(renamed, '/'); i > 0 {
				renamed = renamed[:i]
			}
		}
		if renamed != "" && renamed != info.BestName() {
			fr.QbtName = renamed
		}
	}
	fr.QbtSavePath = fr.SavePath
	fr.QbtQueuePosition = positionNum

	fr.ApplyIPFilter = 1
	fr.NumIncomplete = 0

	ltVersion := fr.LibTorrentVersion
	if ltVersion == "" || strings.HasPrefix(ltVersion, "0.") || strings.HasPrefix(ltVersion, "1.0") {
		fr.Paused = 0
		fr.AutoManaged = 1
	}

	for idx, p := range fr.FilePriority {
		if p != 0 && p != 6 && p != 7 {
			fr.FilePriority[idx] = 1
		}
	}

	if len(fr.FilePriority) != len(info.UpvertedFiles()) {
		fr.ConvertFilePriority(len(info.UpvertedFiles()))
	}

	fr.Pieces = pieces
	fr.Unfinished = nil
}

func delugePieces(fr *Fastresume, info *metainfo.Info) (string, bool) {
	numPieces := info.NumPieces()
	if numPieces == 0 || info.PieceLength <= 0 {
		return "", false
	}

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

func readDelugeLabels(stateDir string) map[string]string {
	path := filepath.Join(stateDir, "..", "label.conf")

	f, err := os.Open(path)
	if err != nil {
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

package clientmigrate

import (
	"os"
	"path/filepath"

	"github.com/autobrr/qui/internal/qbittorrent"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"github.com/zeebo/bencode"
)

type DelugeImport struct {
	opts Options
}

func NewDelugeImporter(opts Options) ClientMigrater {
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
		if err := MkDirIfNotExists(di.opts.QbitDir); err != nil {
			return errors.Wrapf(err, "qbit directory error: %s", di.opts.QbitDir)
		}
	}

	resumeFilePath := filepath.Join(sourceDir, "torrents.fastresume")
	if _, err := os.Stat(resumeFilePath); os.IsNotExist(err) {
		log.Error().Err(err).Msgf("Could not find deluge fastresume file: %s", resumeFilePath)
		return err
	}

	fastresumeFile, err := decodeFastresumeFile(resumeFilePath)
	if err != nil {
		log.Error().Err(err).Msgf("Could not decode deluge fastresume file: %s", resumeFilePath)
		return err
	}

	matches, err := filepath.Glob(filepath.Join(sourceDir, "*.torrent"))
	if err != nil {
		return errors.Wrapf(err, "glob error: %v", matches)
	}

	totalJobs := len(matches)

	log.Info().Msgf("Total torrents to process: %d", totalJobs)

	positionNum := 0
	for torrentID, value := range fastresumeFile {
		torrentNamePath := filepath.Join(sourceDir, torrentID+".torrent")

		// If a file exist in fastresume data but no .torrent file, skip
		if _, err = os.Stat(torrentNamePath); os.IsNotExist(err) {
			log.Error().Err(err).Msgf("%s: skipping because %s not found in source directory", torrentID, torrentNamePath)
			continue
		}

		positionNum++

		torrentOutFile := filepath.Join(di.opts.QbitDir, torrentID+".torrent")

		// If file already exists, skip
		if _, err = os.Stat(torrentOutFile); err == nil {
			log.Info().Msgf("(%d/%d) %s Torrent already exists, skipping", positionNum, totalJobs, torrentID)
			continue
		}

		var fastResume qbittorrent.Fastresume

		strValue, ok := value.(string)
		if !ok {
			log.Error().Msgf("Could not convert value %s to string", value)
			continue
		}

		if err := bencode.DecodeString(strValue, &fastResume); err != nil {
			log.Error().Err(err).Msgf("Could not decode row %s. Continue", torrentID)
			continue
		}

		fastResume.TorrentFilePath = torrentNamePath
		if _, err = os.Stat(fastResume.TorrentFilePath); os.IsNotExist(err) {
			log.Error().Err(err).Msgf("Could not find torrent file %s for %s", fastResume.TorrentFilePath, torrentID)
			continue
		}

		file, err := metainfo.LoadFromFile(torrentNamePath)
		if err != nil {
			log.Error().Err(err).Msgf("Could not load torrent file %s for %s", fastResume.TorrentFilePath, torrentID)
			continue
		}

		metaInfo, err := file.UnmarshalInfo()
		if err != nil {
			log.Error().Err(err).Msgf("Could not unmarshal torrent file %s for %s", fastResume.TorrentFilePath, torrentID)
			continue
		}

		if metaInfo.Files != nil {
			// valid QbtContentLayout = Original, Subfolder, NoSubfolder
			fastResume.QbtContentLayout = "Original"
			// legacy and should be removed sometime with 4.3.X
			fastResume.QbtHasRootFolder = 1
		} else {
			fastResume.QbtContentLayout = "NoSubfolder"
			fastResume.QbtHasRootFolder = 0
		}

		fastResume.QbtRatioLimit = -2000
		fastResume.QbtSeedStatus = 1
		fastResume.QbtSeedingTimeLimit = -2
		fastResume.QbtName = ""
		fastResume.QbtSavePath = fastResume.SavePath
		fastResume.QbtQueuePosition = positionNum

		fastResume.AutoManaged = 0
		fastResume.NumIncomplete = 0
		fastResume.Paused = 0

		fastResume.ConvertFilePriority(len(metaInfo.Files))

		// fill pieces to set as completed
		fastResume.NumPieces = int64(metaInfo.NumPieces())
		fastResume.FillPieces()

		// TODO handle replace paths

		if di.opts.DryRun {
			log.Info().Msgf("dry-run: (%d/%d) successfully imported: %s", positionNum, totalJobs, torrentID)
			continue
		}

		fastResumeOutFile := filepath.Join(di.opts.QbitDir, torrentID+".fastresume")
		if err = fastResume.Encode(fastResumeOutFile); err != nil {
			log.Error().Err(err).Msgf("Could not create qBittorrent fastresume file %s error: %q", fastResumeOutFile, err)
			continue
		}

		if err = CopyFile(fastResume.TorrentFilePath, torrentOutFile); err != nil {
			log.Error().Err(err).Msgf("Could not copy qBittorrent torrent file %s error %q", torrentOutFile, err)
			continue
		}

		log.Info().Msgf("(%d/%d) successfully imported: %s %s", positionNum, totalJobs, torrentID, metaInfo.Name)
	}

	log.Info().Msgf("(%d/%d) successfully imported torrents!", positionNum, totalJobs)

	return nil
}

func decodeFastresumeFile(path string) (map[string]interface{}, error) {
	dat, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var fastresumeFile map[string]interface{}
	if err := bencode.DecodeBytes(dat, &fastresumeFile); err != nil {
		return nil, err
	}

	return fastresumeFile, nil
}

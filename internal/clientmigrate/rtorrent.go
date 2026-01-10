package clientmigrate

import (
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/autobrr/qui/internal/qbittorrent"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"github.com/zeebo/bencode"
)

type RTorrentImport struct {
	opts Options
}

func NewRTorrentImporter(opts Options) ClientMigrater {
	return &RTorrentImport{opts: opts}
}

var (
	rtStateFileExtension         = ".rtorrent"
	libtorrentStateFileExtension = ".libtorrent_resume"
)

func (i *RTorrentImport) Migrate() error {
	torrentsSessionDir := i.opts.SourceDir

	sourceDirInfo, err := os.Stat(torrentsSessionDir)
	if err != nil {
		if os.IsNotExist(err) {
			return errors.Errorf("source directory does not exist: %s", torrentsSessionDir)
		}

		return errors.Wrapf(err, "source directory error: %s", torrentsSessionDir)
	}

	if !sourceDirInfo.IsDir() {
		return errors.Errorf("source is a file, not a directory: %s", torrentsSessionDir)
	}

	matches, err := filepath.Glob(filepath.Join(torrentsSessionDir, "*.torrent"))
	if err != nil {
		return errors.Wrapf(err, "glob error: %s", torrentsSessionDir)
	}

	if len(matches) == 0 {
		log.Info().Msgf("Found 0 files to process in: %s", torrentsSessionDir)
		return nil
	}

	totalJobs := len(matches)

	log.Info().Msgf("Total torrents to process: %d", totalJobs)

	positionNum := 0
	for _, match := range matches {
		positionNum++

		torrentID := getTorrentFileName(match)

		torrentOutFile := filepath.Join(i.opts.QbitDir, torrentID+".torrent")

		// If file already exists, skip
		if _, err = os.Stat(torrentOutFile); err == nil {
			log.Info().Msgf("(%d/%d) %s Torrent already exists, skipping", positionNum, totalJobs, torrentOutFile)
			continue
		}

		if i.opts.DryRun {
			log.Info().Msgf("dry-run: (%d/%d) successfully imported: %s", positionNum, totalJobs, torrentID)
			continue
		}

		file, err := metainfo.LoadFromFile(match)
		if err != nil {
			log.Error().Err(err).Msgf("Could not load torrent file %s for %s", match, torrentID)
			continue
		}

		metaInfo, err := file.UnmarshalInfo()
		if err != nil {
			log.Error().Err(err).Msgf("Could not unmarshal torrent file %s for %s", match, torrentID)
			continue
		}

		// check for FILE.torrent.libtorrent_resume
		resumeFile, err := i.decodeRTorrentLibTorrentResumeFile(match)
		if err != nil {
			log.Error().Err(err).Msgf("Could not decode rtorrent libtorrent resume file %s for %s", match, torrentID)
			continue
		}

		// check for FILE.torrent.rtorrent
		rtFile, err := i.decodeRTorrentFile(match)
		if err != nil {
			log.Error().Err(err).Msgf("Could not decode rtorrent state file %s for %s", match, torrentID)
			continue
		}

		newFastResume := qbittorrent.Fastresume{
			ActiveTime:                getActiveTime(rtFile.Custom.SeedingTime),
			AddedTime:                 strToIntClean(rtFile.Custom.AddTime),
			Allocation:                "sparse",
			ApplyIpFilter:             1,
			AutoManaged:               0,
			CompletedTime:             rtFile.TimestampFinished,
			DisableDHT:                0,
			DisableLSD:                0,
			DisablePEX:                0,
			DownloadRateLimit:         -1,
			FileFormat:                "libtorrent resume file",
			FileVersion:               1,
			FilePriority:              []int{},
			FinishedTime:              int64(time.Since(time.Unix(rtFile.TimestampFinished, 0)).Seconds()),
			LastDownload:              0,
			LastSeenComplete:          rtFile.TimestampFinished,
			LastUpload:                0,
			LibTorrentVersion:         "1.2.11.0",
			MaxConnections:            16777215,
			MaxUploads:                -1,
			NumComplete:               16777215,
			NumDownloaded:             16777215,
			NumIncomplete:             0,
			NumPieces:                 int64(metaInfo.NumPieces()),
			Paused:                    0,
			Peers:                     "",
			Peers6:                    "",
			QbtCategory:               "",
			QbtContentLayout:          "Original",
			QbtFirstLastPiecePriority: 0,
			QbtName:                   "",
			QbtRatioLimit:             -2000,
			QbtSavePath:               rtFile.Directory,
			QbtSeedStatus:             1,
			QbtSeedingTimeLimit:       -2,
			QbtTags:                   []string{},
			SavePath:                  rtFile.Directory,
			SeedMode:                  0,
			SeedingTime:               getActiveTime(rtFile.Custom.SeedingTime),
			SequentialDownload:        0,
			ShareMode:                 0,
			StopWhenReady:             0,
			SuperSeeding:              0,
			TotalDownloaded:           rtFile.TotalDownloaded,
			TotalUploaded:             rtFile.TotalUploaded,
			UploadMode:                0,
			UploadRateLimit:           -1,
			UrlList:                   file.UrlList,

			Path: rtFile.Directory,
		}

		//if file.Info.Files != nil {
		if metaInfo.Files != nil {
			newFastResume.HasFiles = true

			// valid QbtContentLayout = Original, Subfolder, NoSubfolder
			newFastResume.QbtContentLayout = "Original"
			// legacy and should be removed sometime with 4.3.X
			newFastResume.QbtHasRootFolder = 1

			// Fix savepath for torrents with subfolder
			// directory contains the whole torrent path, which gives error in qBit.
			// remove file.sourceDirInfo.name from full path in id.rtorrent directory
			newPath := strings.ReplaceAll(rtFile.Directory, metaInfo.Name, "")

			newFastResume.Path = newPath
			newFastResume.SavePath = newPath
			newFastResume.QbtSavePath = newPath
		} else {
			// if only single file then use NoSubfolder
			newFastResume.HasFiles = false

			newFastResume.QbtContentLayout = "NoSubfolder"
			newFastResume.QbtHasRootFolder = 0
		}

		// handle trackers
		newFastResume.Trackers = i.convertTrackers(*resumeFile)

		newFastResume.ConvertFilePriority(len(metaInfo.Files))

		// fill pieces to set as completed
		newFastResume.FillPieces()

		// Set 20 byte SHA1 hash
		newFastResume.InfoHash = file.HashInfoBytes().Bytes()

		// copy torrent file
		fastResumeOutFile := filepath.Join(i.opts.QbitDir, torrentID+".fastresume")
		if err = newFastResume.Encode(fastResumeOutFile); err != nil {
			log.Error().Err(err).Msgf("Could not create qBittorrent fastresume file %s error: %q", fastResumeOutFile, err)
			continue
		}

		if err = CopyFile(match, torrentOutFile); err != nil {
			log.Error().Err(err).Msgf("Could not copy qBittorrent torrent file %s error %q", torrentOutFile, err)
			continue
		}

		log.Info().Msgf("(%d/%d) successfully imported: %s %s", positionNum, totalJobs, torrentID, metaInfo.Name)
	}

	log.Info().Msgf("(%d/%d) successfully imported torrents!", positionNum, totalJobs)

	return nil
}

// Takes rtorrent custom.seedingtime (UNIX timestamp of seeding start) and converts to elapsed seconds
func getActiveTime(t string) int64 {
	startTime := strToIntClean(t)
	if startTime == 0 {
		return 0
	}
	return time.Now().Unix() - startTime
}

// convertTrackers from rtorrent file spec to qBittorrent fastresume
func (i *RTorrentImport) convertTrackers(trackers RTorrentLibTorrentResumeFile) [][]string {
	var ret [][]string

	for url, status := range trackers.Trackers {
		// skip if dht
		if url == "dht://" {
			continue
		}

		if status["enabled"] == 1 {
			ret = append(ret, []string{url})
		}
	}

	return ret
}

// getTorrentFileName from file. Removes file extension
func getTorrentFileName(file string) string {
	_, fileName := filepath.Split(file)
	trimmed := strings.TrimSuffix(fileName, path.Ext(fileName))
	toLower := strings.ToLower(trimmed)

	return toLower
}

func (i *RTorrentImport) decodeRTorrentLibTorrentResumeFile(path string) (*RTorrentLibTorrentResumeFile, error) {
	dat, err := os.ReadFile(path + libtorrentStateFileExtension)
	if err != nil {
		return nil, err
	}

	var torrentResumeFile RTorrentLibTorrentResumeFile
	if err := bencode.DecodeBytes(dat, &torrentResumeFile); err != nil {
		return nil, err
	}

	return &torrentResumeFile, nil
}

func (i *RTorrentImport) decodeRTorrentFile(path string) (*RTorrentTorrentFile, error) {
	dat, err := os.ReadFile(path + rtStateFileExtension)
	if err != nil {
		return nil, err
	}

	var torrentFile RTorrentTorrentFile
	if err := bencode.DecodeBytes(dat, &torrentFile); err != nil {
		return nil, err
	}

	return &torrentFile, nil
}

// Clean and convert string to int from rtorrent.custom.addtime, seedingtime
func strToIntClean(line string) int64 {
	if line == "" {
		return 0
	}

	s := strings.TrimSuffix(line, "\n")
	i, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return i
}

type RTorrentLibTorrentResumeFile struct {
	Trackers map[string]map[string]int `bencode:"trackers"`
}

type RTorrentTorrentFile struct {
	Custom struct {
		AddTime     string `bencode:"addtime"`
		SeedingTime string `bencode:"seedingtime"`
	} `bencode:"custom"`
	Directory         string `bencode:"directory"`
	TotalDownloaded   int64  `bencode:"total_downloaded"`
	TotalUploaded     int64  `bencode:"total_uploaded"`
	TimestampFinished int64  `bencode:"timestamp.finished"`
	TimestampStarted  int64  `bencode:"timestamp.started"`
}

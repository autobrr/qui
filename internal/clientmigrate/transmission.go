package clientmigrate

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/autobrr/qui/internal/qbittorrent"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"github.com/zeebo/bencode"
)

type TransmissionImport struct {
	opts Options
}

func NewTransmissionImporter(opts Options) ClientMigrater {
	return &TransmissionImport{opts: opts}
}

func (i *TransmissionImport) Migrate() error {
	torrentsDir := i.opts.SourceDir + "/torrents"

	sourceDirInfo, err := os.Stat(torrentsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return errors.Errorf("source directory does not exist: %s", torrentsDir)
		}

		return errors.Wrapf(err, "source directory error: %s", torrentsDir)
	}

	if !sourceDirInfo.IsDir() {
		return errors.Errorf("source is a file, not a directory: %s", torrentsDir)
	}

	matches, err := filepath.Glob(filepath.Join(torrentsDir, "*.torrent"))
	if err != nil {
		return errors.Wrapf(err, "glob error: %s", torrentsDir)
	}

	if len(matches) == 0 {
		log.Info().Msgf("Found 0 files to process in: %s", torrentsDir)
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
			log.Info().Msgf("(%d/%d) %s Torrent already exists, skipping", positionNum, totalJobs, torrentID)
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

		resumeFilePath := filepath.Join(i.opts.SourceDir, "resume", torrentID+".resume")

		// check for FILE.resume
		resumeFile, err := i.decodeResumeFile(resumeFilePath)
		if err != nil {
			log.Error().Err(err).Msgf("Could not decode transmission resume file %s for %s", match, torrentID)
			continue
		}

		newFastResume := qbittorrent.Fastresume{
			ActiveTime:                int64(time.Since(time.Unix(resumeFile.DoneDate, 0)).Seconds()),
			AddedTime:                 resumeFile.AddedDate,
			Allocation:                "sparse",
			ApplyIpFilter:             1,
			AutoManaged:               0,
			CompletedTime:             resumeFile.DoneDate,
			DisableDHT:                0,
			DisableLSD:                0,
			DisablePEX:                0,
			DownloadRateLimit:         -1,
			FileFormat:                "libtorrent resume file",
			FileVersion:               1,
			FilePriority:              []int{},
			FinishedTime:              int64(time.Since(time.Unix(resumeFile.DoneDate, 0)).Seconds()),
			LastDownload:              0,
			LastSeenComplete:          resumeFile.DoneDate,
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
			QbtSavePath:               resumeFile.Destination,
			QbtSeedStatus:             1,
			QbtSeedingTimeLimit:       -2,
			QbtTags:                   []string{"migrated"},
			SavePath:                  resumeFile.Destination,
			SeedMode:                  0,
			SeedingTime:               resumeFile.SeedingTimeSeconds,
			SequentialDownload:        0,
			ShareMode:                 0,
			StopWhenReady:             0,
			SuperSeeding:              0,
			TotalDownloaded:           resumeFile.Downloaded,
			TotalUploaded:             resumeFile.Uploaded,
			UploadMode:                0,
			UploadRateLimit:           -1,
			UrlList:                   file.UrlList,

			//Path: resumeFile.Destination,
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
			// remove file.sourceDirInfo.name from full path directory
			newPath := strings.ReplaceAll(resumeFile.Destination, metaInfo.Name, "")

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
		newFastResume.Trackers = file.UpvertedAnnounceList()

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

func (i *TransmissionImport) decodeResumeFile(path string) (*TransmissionResumeFile, error) {
	dat, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var torrentResumeFile TransmissionResumeFile
	if err := bencode.DecodeBytes(dat, &torrentResumeFile); err != nil {
		return nil, err
	}

	return &torrentResumeFile, nil
}

type TransmissionResumeFile struct {
	Files                  []string                         `bencode:"files"`
	Name                   string                           `bencode:"name"`
	Corrupted              int64                            `bencode:"corrupt"`
	Destination            string                           `bencode:"destination"`
	IncompleteDir          string                           `bencode:"incomplete-dir"`
	Downloaded             int64                            `bencode:"downloaded"`
	Uploaded               int64                            `bencode:"uploaded"`
	Group                  string                           `bencode:"group"`
	BandwidthPriority      int                              `bencode:"bandwidth-priority"`
	Priority               []int                            `bencode:"priority"`
	DoneDate               int64                            `bencode:"done-date"`
	DownloadingTimeSeconds int64                            `bencode:"downloading-time-seconds"`
	Labels                 []string                         `bencode:"labels"`
	MaxPeers               int64                            `bencode:"maxpeers"`
	Paused                 bool                             `bencode:"paused"`
	Peers                  string                           `bencode:"peers2"`
	ActivityDate           int64                            `bencode:"activity-date"`
	AddedDate              int64                            `bencode:"added-date"`
	Dnd                    []int                            `bencode:"dnd"`
	SeedingTimeSeconds     int64                            `bencode:"seeding-time-seconds"`
	Progress               TransmissionResumeFileProgress   `bencode:"progress"`
	IdleLimit              TransmissionResumeFileIdleLimit  `bencode:"idle-limit"`
	RatioLimit             TransmissionResumeFileRatioLimit `bencode:"ratio-limit"`
	SpeedLimitUp           TransmissionResumeFileSpeedLimit `bencode:"speed-limit-up"`
	SpeedLimitDown         TransmissionResumeFileSpeedLimit `bencode:"speed-limit-down"`
}

type TransmissionResumeFileProgress struct {
	Blocks string  `bencode:"blocks"`
	Have   string  `bencode:"have"`
	MTimes []int64 `bencode:"mtimes"`
	Pieces string  `bencode:"pieces"`
}

type TransmissionResumeFileSpeedLimit struct {
	SpeedBPS            int64 `bencode:"speed-limit-seconds"`
	UseGlobalSpeedLimit int64 `bencode:"use-global-speed-limit"`
	UseSpeedLimit       int64 `bencode:"use-speed-limit"`
}

type TransmissionResumeFileRatioLimit struct {
	RatioLimit string `bencode:"ratio-limit"`
	RatioMode  int    `bencode:"ratio-mode"`
}

type TransmissionResumeFileIdleLimit struct {
	IdleLimit int64 `bencode:"idle-limit"`
	IdleMode  int   `bencode:"idle-mode"`
}

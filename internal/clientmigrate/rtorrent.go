package clientmigrate

import (
	"bytes"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/autobrr/go-torrent/bencode"
	"github.com/autobrr/go-torrent/metainfo"

	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
)

type RTorrentImport struct {
	opts Options
}

func NewRTorrentImporter(opts Options) *RTorrentImport {
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

	if !i.opts.DryRun {
		if err := os.MkdirAll(i.opts.QbitDir, os.ModePerm); err != nil {
			return errors.Wrapf(err, "qbit directory error: %s", i.opts.QbitDir)
		}
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
	imported := 0
	failed := 0
	skipped := 0
	for _, match := range matches {
		positionNum++

		file, err := metainfo.LoadFromFile(match)
		if err != nil {
			log.Error().Err(err).Msgf("Could not load torrent file %s", match)
			failed++
			continue
		}

		// an unresolved magnet download's session file has no info dict
		if len(file.InfoBytes) == 0 {
			log.Debug().Msgf("(%d/%d) skipping magnet/meta download session file: %s", positionNum, totalJobs, match)
			skipped++
			continue
		}

		metaInfo, err := file.UnmarshalInfo()
		if err != nil {
			log.Error().Err(err).Msgf("Could not unmarshal torrent file %s", match)
			failed++
			continue
		}

		// qBittorrent requires BT_backup files named by lowercase infohash
		torrentID := file.HashInfoBytes().HexString()

		torrentOutFile := filepath.Join(i.opts.QbitDir, torrentID+".torrent")

		// If file already exists, skip
		if _, err = os.Stat(torrentOutFile); err == nil {
			log.Info().Msgf("(%d/%d) %s Torrent already exists, skipping", positionNum, totalJobs, torrentOutFile)
			skipped++
			continue
		}

		// check for FILE.torrent.libtorrent_resume
		resumeFile, err := i.decodeRTorrentLibTorrentResumeFile(match)
		if err != nil {
			log.Error().Err(err).Msgf("Could not decode rtorrent libtorrent resume file %s for %s", match, torrentID)
			failed++
			continue
		}

		// check for FILE.torrent.rtorrent
		rtFile, err := i.decodeRTorrentFile(match)
		if err != nil {
			log.Error().Err(err).Msgf("Could not decode rtorrent state file %s for %s", match, torrentID)
			failed++
			continue
		}

		// complete means every piece of every wanted (non-off) file is
		// present; files with priority 0 may legitimately be missing
		pieces, complete := rtorrentPieces(resumeFile, &metaInfo)
		if !complete {
			log.Warn().Msgf("(%d/%d) %s is not fully downloaded, skipping: %s", positionNum, totalJobs, metaInfo.Name, match)
			skipped++
			continue
		}

		// directory is stored before shell expansion and can be ~-prefixed
		dir := rtFile.Directory
		if strings.HasPrefix(dir, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				dir = filepath.Join(home, dir[2:])
			}
		}
		dir = filepath.Clean(dir)

		// also catches POSIX session paths on a Windows host: cross-OS
		// migration cannot work because the save paths would be unusable
		if !filepath.IsAbs(dir) {
			log.Warn().Msgf("(%d/%d) %s download directory %q is not an absolute path on this OS, skipping", positionNum, totalJobs, metaInfo.Name, rtFile.Directory)
			skipped++
			continue
		}

		if i.opts.DryRun {
			log.Info().Msgf("dry-run: (%d/%d) would import: %s", positionNum, totalJobs, torrentID)
			imported++
			continue
		}

		// added time: ruTorrent's custom.addtime when present, else rtorrent's
		// first-resume timestamp, else the session file mtime
		addedTime := firstNonZero(
			strToIntClean(rtFile.Custom.AddTime),
			rtFile.TimestampStarted,
			fileModTime(match+rtStateFileExtension),
			time.Now().Unix(),
		)

		// timestamp.finished stays 0 on 0.9.6 for torrents completed via
		// hash-check; the torrent is complete here, so fall back to added time
		finishedTime := firstNonZero(rtFile.TimestampFinished, addedTime)

		// derive every seeding duration from one epoch so qBittorrent's Time
		// Active and Seeding Time columns agree: ruTorrent's custom.seedingtime
		// (start of seeding) when present, else the finished timestamp
		seedingSince := firstNonZero(strToIntClean(rtFile.Custom.SeedingTime), finishedTime)
		seedingDuration := max(time.Now().Unix()-seedingSince, 0)

		// 0.9.6 predates the total_downloaded key
		totalDownloaded := rtFile.TotalDownloaded
		if totalDownloaded == 0 {
			totalDownloaded = metaInfo.TotalLength()
		}

		// auto-managed keeps qBittorrent's queueing and share limits in
		// charge; a stopped torrent must stay out of auto-management or
		// qBittorrent would resume it
		paused := int64(1)
		if rtFile.State == 1 {
			paused = 0
		}

		newFastResume := Fastresume{
			ActiveTime:                seedingDuration,
			AddedTime:                 addedTime,
			Allocation:                "sparse",
			ApplyIPFilter:             1,
			AutoManaged:               1 - paused,
			CompletedTime:             finishedTime,
			DisableDHT:                0,
			DisableLSD:                0,
			DisablePEX:                0,
			DownloadRateLimit:         -1,
			FileFormat:                "libtorrent resume file",
			FileVersion:               1,
			FilePriority:              rtorrentFilePriorities(resumeFile.Files, len(metaInfo.UpvertedFiles())),
			FinishedTime:              seedingDuration,
			LastDownload:              0,
			LastSeenComplete:          finishedTime,
			LastUpload:                0,
			LibTorrentVersion:         "1.2.11.0",
			MaxConnections:            16777215,
			MaxUploads:                -1,
			NumComplete:               16777215,
			NumDownloaded:             16777215,
			NumIncomplete:             0,
			Paused:                    paused,
			Peers:                     "",
			Peers6:                    "",
			QbtCategory:               rtorrentLabel(rtFile.Custom1),
			QbtContentLayout:          "Original",
			QbtFirstLastPiecePriority: 0,
			QbtName:                   "",
			QbtRatioLimit:             -2000,
			QbtSavePath:               dir,
			QbtSeedStatus:             1,
			QbtSeedingTimeLimit:       -2,
			QbtTags:                   []string{"migrated"},
			SavePath:                  dir,
			SeedMode:                  0,
			SeedingTime:               seedingDuration,
			SequentialDownload:        0,
			ShareMode:                 0,
			StopWhenReady:             0,
			SuperSeeding:              0,
			TotalDownloaded:           totalDownloaded,
			TotalUploaded:             rtFile.TotalUploaded,
			UploadMode:                0,
			UploadRateLimit:           -1,
			URLList:                   file.UrlList,
		}

		if metaInfo.IsDir() {
			if filepath.Base(dir) == metaInfo.Name {
				// normal d.directory.set layout: directory includes the
				// torrent's top folder, qBittorrent expects the parent
				savePath := filepath.Dir(dir)
				newFastResume.SavePath = savePath
				newFastResume.QbtSavePath = savePath
				// legacy and should be removed sometime with 4.3.X
				newFastResume.QbtHasRootFolder = 1
			} else {
				// d.directory_base.set layout: files live directly in the
				// directory without the torrent-name folder. qBt-contentLayout
				// only applies to newly added torrents, so a restored torrent
				// needs mapped_files to strip the name folder from each path
				newFastResume.QbtContentLayout = "NoSubfolder"
				newFastResume.QbtHasRootFolder = 0

				files := metaInfo.UpvertedFiles()
				mapped := make([]string, 0, len(files))
				for _, f := range files {
					mapped = append(mapped, strings.Join(f.BestPath(), "/"))
				}
				newFastResume.MappedFiles = mapped
			}
		} else {
			newFastResume.QbtHasRootFolder = 0
		}

		// handle trackers
		newFastResume.Trackers = rtorrentTrackers(file, resumeFile)

		// pieces overlapping only priority-off files stay unset
		newFastResume.Pieces = pieces

		// Set 20 byte SHA1 hash
		newFastResume.InfoHash = file.HashInfoBytes().Bytes()

		// copy torrent file
		fastResumeOutFile := filepath.Join(i.opts.QbitDir, torrentID+".fastresume")
		if err = newFastResume.Encode(fastResumeOutFile); err != nil {
			log.Error().Err(err).Msgf("Could not create qBittorrent fastresume file %s", fastResumeOutFile)
			failed++
			continue
		}

		if err = CopyFile(match, torrentOutFile); err != nil {
			log.Error().Err(err).Msgf("Could not copy qBittorrent torrent file %s", torrentOutFile)
			failed++
			continue
		}

		imported++

		log.Info().Msgf("(%d/%d) successfully imported: %s %s", positionNum, totalJobs, torrentID, metaInfo.Name)
	}

	logImportSummary(i.opts.DryRun, imported, failed, skipped, totalJobs)

	return nil
}

// rtorrentPieces expands the libtorrent_resume bitfield into a qBittorrent
// piece bitfield and reports whether every piece of every wanted file is
// present. The bitfield is the piece count when complete, 0 when empty, and a
// raw MSB-first bitfield string when partial
func rtorrentPieces(resume *RTorrentLibTorrentResumeFile, info *metainfo.Info) (string, bool) {
	numPieces := info.NumPieces()
	if numPieces == 0 || info.PieceLength <= 0 {
		return "", false
	}

	havePiece := make([]bool, numPieces)
	switch v := resume.Bitfield.(type) {
	case int64:
		if v != int64(numPieces) {
			return "", false
		}
		for i := range havePiece {
			havePiece[i] = true
		}
	case string:
		if len(v) < (numPieces+7)/8 {
			return "", false
		}
		for i := range numPieces {
			havePiece[i] = v[i/8]>>(7-i%8)&1 == 1
		}
	default:
		return "", false
	}

	files := info.UpvertedFiles()
	wanted := make([]bool, len(files))
	for i := range wanted {
		wanted[i] = true
	}
	if len(resume.Files) == len(files) {
		for i, f := range resume.Files {
			wanted[i] = f.Priority != 0
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
		if havePiece[p] {
			pieces[p] = 1
		} else if wp[p] {
			complete = false
		}
	}

	return string(pieces), complete
}

// rtorrentFilePriorities maps rtorrent per-file priorities (0 off, 1 normal,
// 2 high) to qBittorrent values (0 skip, 1 normal, 6 high)
func rtorrentFilePriorities(entries []RTorrentResumeFileEntry, fileCount int) []int {
	prios := make([]int, 0, fileCount)

	if len(entries) != fileCount {
		for range fileCount {
			prios = append(prios, 1)
		}
		return prios
	}

	for _, f := range entries {
		switch f.Priority {
		case 0:
			prios = append(prios, 0)
		case 2:
			prios = append(prios, 6)
		default:
			prios = append(prios, 1)
		}
	}

	return prios
}

// rtorrentLabel decodes a ruTorrent label from custom1, which ruTorrent
// stores rawurlencoded; PathUnescape keeps a literal + a plus
func rtorrentLabel(custom1 string) string {
	label, err := url.PathUnescape(custom1)
	if err != nil {
		return custom1
	}
	return label
}

// rtorrentTrackers rebuilds the tracker tiers from the torrent's own
// announce-list, dropping trackers the resume data marks disabled, and
// appends enabled trackers that were added at runtime. An empty result stays
// nil so the encoded fastresume does not override the .torrent's trackers
func rtorrentTrackers(file *metainfo.MetaInfo, resume *RTorrentLibTorrentResumeFile) [][]string {
	enabled := func(trackerURL string) bool {
		status, ok := resume.Trackers[trackerURL]
		if !ok {
			return true
		}
		return status["enabled"] != 0
	}

	known := make(map[string]struct{})

	var tiers [][]string
	for _, tier := range file.UpvertedAnnounceList() {
		var kept []string
		for _, trackerURL := range tier {
			known[trackerURL] = struct{}{}
			if enabled(trackerURL) {
				kept = append(kept, trackerURL)
			}
		}
		if len(kept) > 0 {
			tiers = append(tiers, kept)
		}
	}

	// trackers added at runtime only exist in the resume data
	var extras []string
	for trackerURL, status := range resume.Trackers {
		if trackerURL == "dht://" {
			continue
		}
		if _, ok := known[trackerURL]; ok {
			continue
		}
		if status["enabled"] != 0 {
			extras = append(extras, trackerURL)
		}
	}
	slices.Sort(extras)

	for _, trackerURL := range extras {
		tiers = append(tiers, []string{trackerURL})
	}

	return tiers
}

func fileModTime(path string) int64 {
	fi, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return fi.ModTime().Unix()
}

func (i *RTorrentImport) decodeRTorrentLibTorrentResumeFile(path string) (*RTorrentLibTorrentResumeFile, error) {
	dat, err := os.ReadFile(path + libtorrentStateFileExtension)
	if err != nil {
		return nil, err
	}

	var torrentResumeFile RTorrentLibTorrentResumeFile
	if err := bencode.NewDecoder(bytes.NewReader(dat)).Decode(&torrentResumeFile); err != nil {
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
	if err := bencode.NewDecoder(bytes.NewReader(dat)).Decode(&torrentFile); err != nil {
		return nil, err
	}

	return &torrentFile, nil
}

// Clean and convert string to int from rtorrent.custom.addtime, seedingtime
func strToIntClean(line string) int64 {
	s := strings.TrimSpace(line)
	if s == "" {
		return 0
	}

	i, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return i
}

type RTorrentLibTorrentResumeFile struct {
	Bitfield any                       `bencode:"bitfield"`
	Files    []RTorrentResumeFileEntry `bencode:"files"`
	Trackers map[string]map[string]int `bencode:"trackers"`
}

type RTorrentResumeFileEntry struct {
	Completed int64 `bencode:"completed"`
	Mtime     int64 `bencode:"mtime"`
	Priority  int64 `bencode:"priority"`
}

type RTorrentTorrentFile struct {
	Complete int64 `bencode:"complete"`
	Custom   struct {
		AddTime     string `bencode:"addtime"`
		SeedingTime string `bencode:"seedingtime"`
	} `bencode:"custom"`
	Custom1           string `bencode:"custom1"`
	Directory         string `bencode:"directory"`
	State             int64  `bencode:"state"`
	TotalDownloaded   int64  `bencode:"total_downloaded"`
	TotalUploaded     int64  `bencode:"total_uploaded"`
	TimestampFinished int64  `bencode:"timestamp.finished"`
	TimestampStarted  int64  `bencode:"timestamp.started"`
}

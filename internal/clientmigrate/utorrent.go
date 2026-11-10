package clientmigrate

import (
	"bytes"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf16"

	"github.com/autobrr/go-torrent/bencode"
	"github.com/autobrr/go-torrent/metainfo"

	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
)

type UTorrentImport struct {
	opts Options
}

func NewUTorrentImporter(opts Options) *UTorrentImport {
	return &UTorrentImport{opts: opts}
}

func (i *UTorrentImport) Migrate() error {
	sourceDir := i.opts.SourceDir

	entries, err := i.loadResume()
	if err != nil {
		return err
	}

	totalJobs := len(entries)

	log.Info().Msgf("Total torrents to process: %d", totalJobs)

	positionNum := 0
	imported := 0
	failed := 0
	skipped := 0
	for _, key := range slices.Sorted(maps.Keys(entries)) {
		positionNum++

		var item UTorrentResumeItem
		if err := bencode.NewDecoder(bytes.NewReader(entries[key])).Decode(&item); err != nil {
			log.Error().Err(err).Msgf("Could not decode resume entry %s", key)
			failed++
			continue
		}

		torrentPath := utorrentTorrentPath(sourceDir, decodeCESU8(key))

		file, err := metainfo.LoadFromFile(torrentPath)
		if err != nil {
			log.Error().Err(err).Msgf("Could not load torrent file %s", torrentPath)
			failed++
			continue
		}

		metaInfo, err := file.UnmarshalInfo()
		if err != nil {
			log.Error().Err(err).Msgf("Could not unmarshal torrent file %s", torrentPath)
			failed++
			continue
		}

		// v2 and hybrid torrents double the prio list and lose their merkle
		// trees in this format
		if metaInfo.MetaVersion == 2 {
			log.Warn().Msgf("(%d/%d) %s is a BitTorrent v2 torrent, not supported by this importer, skipping", positionNum, totalJobs, metaInfo.BestName())
			skipped++
			continue
		}

		// qBittorrent requires BT_backup files named by infohash
		torrentID := file.HashInfoBytes().HexString()

		torrentOutFile := filepath.Join(i.opts.QbitDir, torrentID+".torrent")

		// If file already exists, skip
		if _, err = os.Stat(torrentOutFile); err == nil {
			log.Info().Msgf("(%d/%d) %s Torrent already exists, skipping", positionNum, totalJobs, torrentID)
			skipped++
			continue
		}

		// resume data that does not belong to this torrent file would import
		// piece state for different content
		if !bytes.Equal(item.Info, file.HashInfoBytes().Bytes()) {
			log.Warn().Msgf("(%d/%d) %s resume data does not match the torrent file, skipping", positionNum, totalJobs, torrentID)
			skipped++
			continue
		}

		// complete means every piece of every wanted (non-skip) file has its
		// have-bit set; skipped files may legitimately be missing
		pieces, complete := utorrentPieces(&item, &metaInfo)
		if !complete {
			log.Warn().Msgf("(%d/%d) %s is not fully downloaded, skipping: %s", positionNum, totalJobs, metaInfo.BestName(), torrentPath)
			skipped++
			continue
		}

		// also catches Windows save paths on a Unix host that no
		// --replace-path rule maps
		contentPath := filepath.Clean(replacePath(decodeCESU8(item.Path), i.opts.PathReplaces))
		if !filepath.IsAbs(contentPath) {
			log.Warn().Msgf("(%d/%d) %s save path %q is not an absolute path on this OS, skipping, map it with --replace-path", positionNum, totalJobs, metaInfo.BestName(), item.Path)
			skipped++
			continue
		}

		savePath, mappedFiles, err := utorrentLayout(&item, contentPath, &metaInfo, i.opts.PathReplaces)
		if err != nil {
			log.Warn().Err(err).Msgf("(%d/%d) %s has a moved file at an unusable path, skipping, map it with --replace-path", positionNum, totalJobs, metaInfo.BestName())
			skipped++
			continue
		}

		if i.opts.DryRun {
			log.Info().Msgf("dry-run: (%d/%d) would import: %s", positionNum, totalJobs, torrentID)
			imported++
			continue
		}

		newFastResume := utorrentFastresume(&item, file, &metaInfo, savePath, mappedFiles, pieces)

		fastResumeOutFile := filepath.Join(i.opts.QbitDir, torrentID+".fastresume")
		if err = newFastResume.Encode(fastResumeOutFile); err != nil {
			log.Error().Err(err).Msgf("Could not create qBittorrent fastresume file %s", fastResumeOutFile)
			failed++
			continue
		}

		if err = CopyFile(torrentPath, torrentOutFile); err != nil {
			log.Error().Err(err).Msgf("Could not copy qBittorrent torrent file %s", torrentOutFile)
			failed++
			continue
		}

		imported++

		log.Info().Msgf("(%d/%d) successfully imported: %s %s", positionNum, totalJobs, torrentID, metaInfo.BestName())
	}

	logImportSummary(i.opts.DryRun, imported, failed, skipped, totalJobs)

	return nil
}

// loadResume checks the source and qBittorrent dirs and returns the
// resume.dat entries
func (i *UTorrentImport) loadResume() (map[string]bencode.Bytes, error) {
	sourceDir := i.opts.SourceDir

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

	if !i.opts.DryRun {
		if err := os.MkdirAll(i.opts.QbitDir, os.ModePerm); err != nil {
			return nil, errors.Wrapf(err, "qbit directory error: %s", i.opts.QbitDir)
		}
	}

	resumePath := filepath.Join(sourceDir, "resume.dat")

	entries, err := decodeUTorrentResumeFile(resumePath)
	if err != nil {
		return nil, errors.Wrapf(err, "could not decode utorrent resume file: %s", resumePath)
	}

	return entries, nil
}

// utorrentFastresume builds the qBittorrent resume data for a complete
// uTorrent resume entry
func utorrentFastresume(item *UTorrentResumeItem, file *metainfo.MetaInfo, info *metainfo.Info, savePath string, mappedFiles []string, pieces string) Fastresume {
	completedTime := firstNonZero(item.CompletedOn, item.AddedOn)

	// stopped (0) and paused (3) stay paused out of auto-management;
	// force-started (1) runs outside qBittorrent's queue like in uTorrent
	paused := int64(0)
	if item.Started == 0 || item.Started == 3 {
		paused = 1
	}
	autoManaged := int64(1)
	if paused == 1 || item.Started == 1 {
		autoManaged = 0
	}

	// wanted_ratio is already the ratio * 1000 that qBt-ratioLimit uses
	ratioLimit := int64(-2000)
	if item.OverrideSeedSettings != 0 {
		ratioLimit = item.WantedRatio
	}

	fr := Fastresume{
		ActiveTime:                item.Runtime,
		AddedTime:                 item.AddedOn,
		Allocation:                "sparse",
		ApplyIPFilter:             1,
		AutoManaged:               autoManaged,
		CompletedTime:             completedTime,
		DisableDHT:                0,
		DisableLSD:                0,
		DisablePEX:                0,
		DownloadRateLimit:         utorrentSpeedLimit(item.DownSpeed),
		FileFormat:                "libtorrent resume file",
		FileVersion:               1,
		FilePriority:              utorrentFilePriorities(item.Prio, len(info.UpvertedFiles())),
		FinishedTime:              item.SeedTime,
		LastDownload:              0,
		LastSeenComplete:          firstNonZero(item.LastSeenComplete, completedTime),
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
		QbtCategory:               utorrentLabel(item),
		QbtContentLayout:          "Original",
		QbtFirstLastPiecePriority: 0,
		QbtName:                   utorrentName(item, info),
		QbtRatioLimit:             ratioLimit,
		QbtSavePath:               savePath,
		QbtSeedStatus:             1,
		QbtSeedingTimeLimit:       -2,
		QbtTags:                   []string{"migrated"},
		SavePath:                  savePath,
		SeedMode:                  0,
		SeedingTime:               item.SeedTime,
		SequentialDownload:        0,
		ShareMode:                 0,
		StopWhenReady:             0,
		SuperSeeding:              0,
		TotalDownloaded:           item.Downloaded,
		TotalUploaded:             item.Uploaded,
		UploadMode:                0,
		UploadRateLimit:           utorrentSpeedLimit(item.UpSpeed),
		URLList:                   file.UrlList,
		MappedFiles:               mappedFiles,
	}

	if info.IsDir() {
		// legacy and should be removed sometime with 4.3.X
		fr.QbtHasRootFolder = 1
	} else {
		fr.QbtHasRootFolder = 0
	}

	fr.Trackers = utorrentTrackers(file, item.Trackers)

	// pieces overlapping only skipped files stay unset
	fr.Pieces = pieces

	// Set 20 byte SHA1 hash
	fr.InfoHash = file.HashInfoBytes().Bytes()

	return fr
}

// utorrentTorrentPath resolves a resume.dat key to its .torrent file. Keys are
// relative to the uTorrent dir, or absolute when the torrent was stored
// elsewhere; an absolute path that no longer resolves (moved dir, other OS)
// falls back to the same file name inside the source dir
func utorrentTorrentPath(sourceDir, key string) string {
	p := key
	if !filepath.IsAbs(p) {
		p = filepath.Join(sourceDir, p)
	}
	if _, err := os.Stat(p); err == nil {
		return p
	}

	return filepath.Join(sourceDir, key[strings.LastIndexAny(key, `/\`)+1:])
}

// utorrentPieces expands the resume entry's have bitfield (one bit per piece,
// LSB-first within each byte) into a qBittorrent piece bitfield and reports
// whether every piece of every wanted (non-skip) file is present
func utorrentPieces(item *UTorrentResumeItem, info *metainfo.Info) (string, bool) {
	numPieces := info.NumPieces()
	if numPieces == 0 || info.PieceLength <= 0 {
		return "", false
	}

	if len(item.Have) < (numPieces+7)/8 {
		return "", false
	}

	files := info.UpvertedFiles()
	wanted := make([]bool, len(files))
	for i := range wanted {
		wanted[i] = true
	}
	if len(item.Prio) == len(files) {
		for i := range files {
			wanted[i] = utorrentPriority(item.Prio[i]) != 0
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
		if item.Have[p/8]>>(p%8)&1 == 1 {
			pieces[p] = 1
		} else if wp[p] {
			complete = false
		}
	}

	return string(pieces), complete
}

// utorrentPriority maps a uTorrent file priority byte (0 skip, 1-8 normal,
// 9-15 high, the 0x80 flag also marks skip) to qBittorrent values (0 skip,
// 1 normal, 6 high)
func utorrentPriority(p byte) int {
	switch {
	case p == 0 || p&0x80 != 0:
		return 0
	case p > 8:
		return 6
	default:
		return 1
	}
}

func utorrentFilePriorities(prio string, fileCount int) []int {
	prios := make([]int, fileCount)
	for i := range prios {
		prios[i] = 1
	}

	if len(prio) == fileCount {
		for i := range prios {
			prios[i] = utorrentPriority(prio[i])
		}
	}

	return prios
}

// utorrentLayout splits the content path into qBittorrent's save path and
// mapped_files. path is the content root: the file itself for single-file
// torrents, the torrent folder for multi-file ones. A renamed root and the
// per-file targets renames only exist in the resume data, so the data on disk
// needs mapped_files. Targets moved out of the content root are absolute and
// go through the same --replace-path rules as the content path; one that stays
// absolute for another OS is an error
func utorrentLayout(item *UTorrentResumeItem, contentPath string, info *metainfo.Info, rules []PathReplace) (string, []string, error) {
	savePath := filepath.Dir(contentPath)
	root := filepath.Base(contentPath)

	if !info.IsDir() {
		if root != info.BestName() {
			return savePath, []string{root}, nil
		}
		return savePath, nil, nil
	}

	// targets entries are [index, path...]; the path is relative to the
	// content root or absolute when the file was moved out of it
	targets := make(map[int]string)
	for _, t := range item.Targets {
		if len(t) < 2 {
			continue
		}
		idx, ok := t[0].(int64)
		if !ok {
			continue
		}
		parts := make([]string, 0, len(t)-1)
		for _, part := range t[1:] {
			if s, ok := part.(string); ok {
				parts = append(parts, decodeCESU8(s))
			}
		}
		if len(parts) == 0 {
			continue
		}

		target := replacePath(strings.Join(parts, "/"), rules)
		if foreignAbs(target) {
			return "", nil, errors.Errorf("file %d target %q is not an absolute path on this OS", idx, target)
		}
		if filepath.IsAbs(target) {
			targets[int(idx)] = filepath.Clean(target)
		} else {
			targets[int(idx)] = root + "/" + path.Clean(sourceSlash(target))
		}
	}

	if root == info.BestName() && len(targets) == 0 {
		return savePath, nil, nil
	}

	files := info.UpvertedFiles()
	mapped := make([]string, len(files))
	for i, f := range files {
		if target, ok := targets[i]; ok {
			mapped[i] = target
		} else {
			mapped[i] = root + "/" + strings.Join(f.BestPath(), "/")
		}
	}

	return savePath, mapped, nil
}

// utorrentName returns the renamed torrent name, or "" when the torrent keeps
// its metainfo name
func utorrentName(item *UTorrentResumeItem, info *metainfo.Info) string {
	caption := decodeCESU8(item.Caption)
	if caption == info.BestName() {
		return ""
	}
	return caption
}

// utorrentLabel returns the uTorrent label as the qBittorrent category. 3.x
// keeps a labels list next to the legacy label string
func utorrentLabel(item *UTorrentResumeItem) string {
	if item.Label != "" {
		return decodeCESU8(item.Label)
	}
	for _, label := range item.Labels {
		if label != "" {
			return decodeCESU8(label)
		}
	}
	return ""
}

// utorrentSpeedLimit maps a per-torrent uTorrent speed limit in bytes/sec to
// the qBittorrent rate limit field, -1 (no limit) when unset
func utorrentSpeedLimit(limit int64) int64 {
	if limit > 0 {
		return limit
	}
	return -1
}

// utorrentTrackers rebuilds the tracker tiers from the torrent's own
// announce-list, dropping trackers the user removed in uTorrent, and appends
// trackers that were added there. The resume list is a flat list of URLs (or
// a whitespace-separated string), so it carries no tiers of its own
func utorrentTrackers(file *metainfo.MetaInfo, raw any) [][]string {
	var urls []string
	var collect func(v any)
	collect = func(v any) {
		switch t := v.(type) {
		case string:
			urls = append(urls, strings.Fields(t)...)
		case []any:
			for _, e := range t {
				collect(e)
			}
		}
	}
	collect(raw)

	if len(urls) == 0 {
		return file.UpvertedAnnounceList()
	}

	listed := make(map[string]struct{}, len(urls))
	for _, u := range urls {
		listed[u] = struct{}{}
	}

	known := make(map[string]struct{})

	var tiers [][]string
	for _, tier := range file.UpvertedAnnounceList() {
		var kept []string
		for _, trackerURL := range tier {
			known[trackerURL] = struct{}{}
			if _, ok := listed[trackerURL]; ok {
				kept = append(kept, trackerURL)
			}
		}
		if len(kept) > 0 {
			tiers = append(tiers, kept)
		}
	}

	for _, trackerURL := range urls {
		if _, ok := known[trackerURL]; ok {
			continue
		}
		known[trackerURL] = struct{}{}
		tiers = append(tiers, []string{trackerURL})
	}

	return tiers
}

// decodeCESU8 converts the CESU-8 surrogate pairs uTorrent writes for
// characters outside the BMP (emoji and the like) into regular UTF-8
func decodeCESU8(s string) string {
	if !strings.Contains(s, "\xed") {
		return s
	}

	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if i+5 < len(s) && s[i] == 0xed && s[i+1]&0xf0 == 0xa0 && s[i+3] == 0xed && s[i+4]&0xf0 == 0xb0 {
			hi := rune(0xd000) | rune(s[i+1]&0x3f)<<6 | rune(s[i+2]&0x3f)
			lo := rune(0xd000) | rune(s[i+4]&0x3f)<<6 | rune(s[i+5]&0x3f)
			b.WriteRune(utf16.DecodeRune(hi, lo))
			i += 5
			continue
		}
		b.WriteByte(s[i])
	}

	return b.String()
}

// decodeUTorrentResumeFile returns the raw resume entry per .torrent key.
// resume.dat mixes torrent entries with meta keys like .fileguard and rec,
// and magnet links without metadata have no .torrent to import
func decodeUTorrentResumeFile(path string) (map[string]bencode.Bytes, error) {
	dat, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var resume map[string]bencode.Bytes
	if err := bencode.NewDecoder(bytes.NewReader(dat)).Decode(&resume); err != nil {
		return nil, err
	}

	entries := make(map[string]bencode.Bytes, len(resume))
	for key, raw := range resume {
		if strings.HasSuffix(strings.ToLower(key), ".torrent") {
			entries[key] = raw
		}
	}

	return entries, nil
}

type UTorrentResumeItem struct {
	AddedOn              int64    `bencode:"added_on"`
	Caption              string   `bencode:"caption"`
	CompletedOn          int64    `bencode:"completed_on"`
	Downloaded           int64    `bencode:"downloaded"`
	DownSpeed            int64    `bencode:"downspeed"`
	Have                 string   `bencode:"have"`
	Info                 []byte   `bencode:"info"`
	Label                string   `bencode:"label"`
	Labels               []string `bencode:"labels"`
	LastSeenComplete     int64    `bencode:"last_seen_complete"`
	OverrideSeedSettings int64    `bencode:"override_seedsettings"`
	Path                 string   `bencode:"path"`
	Prio                 string   `bencode:"prio"`
	Runtime              int64    `bencode:"runtime"`
	SeedTime             int64    `bencode:"seedtime"`
	Started              int64    `bencode:"started"`
	Targets              [][]any  `bencode:"targets"`
	Trackers             any      `bencode:"trackers"`
	Uploaded             int64    `bencode:"uploaded"`
	UpSpeed              int64    `bencode:"upspeed"`
	WantedRatio          int64    `bencode:"wanted_ratio"`
}

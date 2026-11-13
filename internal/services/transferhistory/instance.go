// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transferhistory

import (
	"maps"
	"slices"
	"time"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/trackergroup"
	"github.com/autobrr/qui/pkg/releases"
)

const (
	day = 24 * time.Hour
	// clockSkew is how far the clock of qBittorrent can be from the clock of qui.
	clockSkew = time.Hour
)

func hourOf(t time.Time) int64 { return floorDiv(t.Unix(), 3600) }
func dayOf(t time.Time) int64  { return floorDiv(t.Unix(), 86400) }

func floorDiv(a, b int64) int64 {
	q := a / b
	if a%b != 0 && a < 0 {
		q--
	}
	return q
}

type bytesMoved struct{ uploaded, downloaded int64 }

func (b *bytesMoved) add(up, down int64) {
	b.uploaded += up
	b.downloaded += down
}

type torrentDayKey struct {
	hash string
	day  int64
}

type totalKey struct {
	bucket    int64
	domain    string
	public    bool
	category  string
	crossSeed bool
}

// instance is the state of one instance in memory: the last values of each
// torrent, and the changes since the last write.
type instance struct {
	id              int
	firstRecordedAt time.Time
	lastReadingAt   time.Time
	// writes is the number of writes that stored the instance.
	writes int64
	// resumed is set until the first reading after a load of stored history.
	resumed     bool
	lastWriteAt time.Time
	// writeTimeout is the time limit of the next write, or zero for dbTimeout.
	writeTimeout time.Duration
	records      map[string]*models.TransferHistoryRecord

	// changedRecords changed in more than their last counters.
	changedRecords  map[string]*models.TransferHistoryRecord
	changedCounters map[string]*models.TransferHistoryRecord
	torrentDays     map[torrentDayKey]*bytesMoved
	trackerHours    map[totalKey]*bytesMoved
	categoryDays    map[totalKey]*bytesMoved
	gaps            map[int64]int64
	// unwritten is set from the first reading until a write stores the instance.
	unwritten bool
}

func newInstance(id int, row *models.TransferHistoryInstance, records []*models.TransferHistoryRecord) *instance {
	inst := &instance{id: id, records: make(map[string]*models.TransferHistoryRecord, len(records))}
	inst.reset()
	for _, r := range records {
		inst.records[r.Hash] = r
	}
	if row != nil {
		inst.firstRecordedAt = row.FirstRecordedAt
		inst.lastReadingAt = row.LastReadingAt
		inst.writes = row.Writes
		inst.resumed = true
	}
	return inst
}

func (inst *instance) reset() {
	inst.changedRecords = make(map[string]*models.TransferHistoryRecord)
	inst.changedCounters = make(map[string]*models.TransferHistoryRecord)
	inst.torrentDays = make(map[torrentDayKey]*bytesMoved)
	inst.trackerHours = make(map[totalKey]*bytesMoved)
	inst.categoryDays = make(map[totalKey]*bytesMoved)
	inst.gaps = make(map[int64]int64)
	inst.unwritten = false
}

// apply adds the transfer since the last reading to the changes.
func (inst *instance) apply(r Reading, mapper *trackergroup.Mapper, parser *releases.Parser) {
	first := inst.firstRecordedAt.IsZero()
	previous := inst.lastReadingAt
	if first {
		inst.firstRecordedAt = r.Time
	} else {
		inst.addGap(r)
	}
	inst.lastReadingAt = r.Time
	inst.resumed = false
	inst.unwritten = true

	hour, today := hourOf(r.Time), dayOf(r.Time)
	for i := range r.Torrents {
		t := &r.Torrents[i]
		rec, seen := inst.records[t.Hash]
		var up, down int64
		if !seen {
			// A torrent that qBittorrent had before the last reading, but left
			// out of it, did not move its bytes since then.
			rec = newRecord(t, mapper, r.Time, first || t.AddedAt.Before(previous.Add(-clockSkew)))
			inst.records[t.Hash] = rec
			if rec.BaselineUploaded == 0 && rec.BaselineDownloaded == 0 {
				up, down = t.Uploaded, t.Downloaded
			}
		} else {
			up, down = max(t.Uploaded-rec.LastUploaded, 0), max(t.Downloaded-rec.LastDownloaded, 0)
			if up > 0 && rec.UploadFirst24h != nil && r.Time.Before(t.AddedAt.Add(day)) {
				rec.UploadFirst24h = new(*rec.UploadFirst24h + up)
				inst.changedRecords[t.Hash] = rec
			}
		}

		// The flag of the bytes is the flag before this reading.
		crossSeed := rec.StatisticsCrossSeed != nil && *rec.StatisticsCrossSeed
		if !seen || t.Uploaded != rec.LastUploaded || t.Downloaded != rec.LastDownloaded {
			inst.changedCounters[t.Hash] = rec
		}
		if updateRecord(rec, t, mapper, parser) || !seen {
			inst.changedRecords[t.Hash] = rec
		}
		if up == 0 && down == 0 {
			continue
		}
		public := rec.Private != nil && !*rec.Private
		addTo(inst.torrentDays, torrentDayKey{t.Hash, today}, up, down)
		addTo(inst.trackerHours, totalKey{bucket: hour, domain: rec.TrackerDomain, public: public, crossSeed: crossSeed}, up, down)
		addTo(inst.categoryDays, totalKey{bucket: today, domain: rec.TrackerDomain, public: public, category: rec.Category, crossSeed: crossSeed}, up, down)
	}
}

// newRecord returns the record of a torrent at its first sight. Its bytes go
// into the Baseline when inBaseline is set.
func newRecord(t *Torrent, mapper *trackergroup.Mapper, now time.Time, inBaseline bool) *models.TransferHistoryRecord {
	rec := &models.TransferHistoryRecord{Hash: t.Hash}
	if inBaseline {
		rec.BaselineUploaded, rec.BaselineDownloaded = t.Uploaded, t.Downloaded
		rec.BaselineTrackerDomain, rec.BaselineCategory = mapper.StoredDomain(t.TrackerDomains), t.Category
	}
	if !t.AddedAt.IsZero() && now.Before(t.AddedAt.Add(day)) {
		rec.UploadFirst24h = new(t.Uploaded)
	}
	return rec
}

// addGap stores a Recording gap that ends at r: the time since the last
// stored reading of a resumed instance, or since the first failed read.
func (inst *instance) addGap(r Reading) {
	var start time.Time
	if inst.resumed {
		start = inst.lastReadingAt
	}
	if !r.FailedSince.IsZero() && (start.IsZero() || r.FailedSince.Before(start)) {
		start = r.FailedSince
	}
	if start.IsZero() {
		return
	}
	if start.Before(inst.lastReadingAt) {
		start = inst.lastReadingAt
	}
	if gap := r.Time.Sub(start); gap >= minGap {
		inst.gaps[hourOf(r.Time)] += int64(gap / time.Second)
	}
}

// updateRecord copies the values of t into rec and reports whether one other
// than the last counters changed.
func updateRecord(rec *models.TransferHistoryRecord, t *Torrent, mapper *trackergroup.Mapper, parser *releases.Parser) bool {
	changed := false
	set := func(dst *string, v string) {
		if *dst != v {
			*dst = v
			changed = true
		}
	}
	if rec.Name != t.Name || rec.ContentType == "" {
		release := parser.Parse(t.Name)
		// A name is parsed once in the life of a record, so the cache only
		// holds memory: 50,000 names on the first reading hold over 100 MB.
		parser.Clear(t.Name)
		rec.ReleaseGroup = release.Group
		rec.Resolution = release.Resolution
		rec.Source = releases.NormalizeSource(release.Source)
		rec.ContentType = string(releases.DetermineContentType(release).ContentType)
		changed = true
	}
	set(&rec.Name, t.Name)
	set(&rec.TrackerDomain, mapper.StoredDomain(t.TrackerDomains))
	set(&rec.Category, t.Category)
	if t.Private != nil && (rec.Private == nil || *rec.Private != *t.Private) {
		rec.Private = new(*t.Private)
		changed = true
	}
	if rec.Size != t.Size {
		rec.Size = t.Size
		changed = true
	}
	if rec.StatisticsCrossSeed == nil && t.Complete {
		rec.StatisticsCrossSeed = new(isStatisticsCrossSeed(t))
		changed = true
	}
	rec.LastUploaded, rec.LastDownloaded = t.Uploaded, t.Downloaded
	return changed
}

// isStatisticsCrossSeed reports whether a complete torrent downloaded less
// than 1% of its size.
func isStatisticsCrossSeed(t *Torrent) bool {
	return t.Downloaded*100 < t.Size
}

func addTo[K comparable](m map[K]*bytesMoved, key K, up, down int64) {
	b := m[key]
	if b == nil {
		b = &bytesMoved{}
		m[key] = b
	}
	b.add(up, down)
}

func (inst *instance) hasChanges() bool {
	return inst.unwritten || len(inst.changedRecords) > 0 || len(inst.changedCounters) > 0
}

// changes returns the write of the changes since the last write. The records
// in it are the records of inst: the write must finish before the next apply.
func (inst *instance) changes() *models.TransferHistoryWrite {
	w := &models.TransferHistoryWrite{
		InstanceID:      inst.id,
		FirstRecordedAt: inst.firstRecordedAt,
		LastReadingAt:   inst.lastReadingAt,
		Number:          inst.writes + 1,
		Records:         slices.Collect(maps.Values(inst.changedRecords)),
		Counters:        make([]*models.TransferHistoryRecord, 0, len(inst.changedCounters)),
	}
	for hash, rec := range inst.changedCounters {
		w.Counters = append(w.Counters, rec)
		// A record that a write with a lost answer stored has no id yet.
		if _, listed := inst.changedRecords[hash]; !listed && rec.ID == 0 {
			w.Records = append(w.Records, rec)
		}
	}
	for k, b := range inst.torrentDays {
		w.TorrentDays = append(w.TorrentDays, models.TransferHistoryTorrentDay{Record: inst.records[k.hash], Day: k.day, Uploaded: b.uploaded, Downloaded: b.downloaded})
	}
	w.TrackerHours = totals(inst.trackerHours)
	w.CategoryDays = totals(inst.categoryDays)
	for hour, seconds := range inst.gaps {
		w.Gaps = append(w.Gaps, models.TransferHistoryGap{Hour: hour, Seconds: seconds})
	}
	return w
}

func totals(m map[totalKey]*bytesMoved) []models.TransferHistoryTotal {
	out := make([]models.TransferHistoryTotal, 0, len(m))
	for k, b := range m {
		out = append(out, models.TransferHistoryTotal{
			Bucket: k.bucket, TrackerDomain: k.domain, Public: k.public, Category: k.category,
			StatisticsCrossSeed: k.crossSeed, Uploaded: b.uploaded, Downloaded: b.downloaded,
		})
	}
	return out
}

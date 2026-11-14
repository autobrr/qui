// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transferhistory

import (
	"maps"
	"slices"
	"strings"
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

// movedKey holds the values of the bytes of one torrent in one hour that the
// totals group by. A part of a write sums the bytes of its torrents only.
type movedKey struct {
	record    *models.TransferHistoryRecord
	hour      int64
	domain    string
	public    bool
	category  string
	crossSeed bool
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
	// records holds the record of each torrent that is in qBittorrent, and
	// deleted holds the record of each other torrent, both by hash.
	records map[string]*models.TransferHistoryRecord
	deleted map[string]*models.TransferHistoryRecord

	// completeAt is the time of the last reading that a write stored with
	// all its parts, or zero before the first such write. loaded is set from a
	// load of stored history until the next reading. That reading compares a
	// torrent without a stored record with completeAt, because a part that
	// qui did not store can have held its record.
	completeAt time.Time
	loaded     bool
	// deletedLate holds the time of the last delete of each record that a
	// part of a write after completeAt marked deleted, by hash. The stored
	// state of the record is that of that reading. It is set from a load until
	// the next reading.
	deletedLate map[string]time.Time
	// unsure is what the parts whose outcome qui does not know can have
	// stored, since the last write that stored all its parts.
	unsure unsure

	// changedRecords changed in more than their last counters.
	changedRecords  map[*models.TransferHistoryRecord]struct{}
	changedCounters map[*models.TransferHistoryRecord]struct{}
	moved           map[movedKey]*bytesMoved
	// continued holds the record of each magnet whose life continued in the
	// deleted record of its hash, with that record. A part holds both.
	continued     map[*models.TransferHistoryRecord]*models.TransferHistoryRecord
	gaps          map[int64]int64
	hashChanges   []models.TransferHistoryHashChange
	events        []models.TransferHistoryEvent
	libraryDays   map[int64]*models.TransferHistoryLibraryDay
	instanceHours map[int64]*models.TransferHistoryInstanceHour
	// unwritten is set from the first reading until a write stores the instance.
	unwritten bool
}

func newInstance(id int, row *models.TransferHistoryInstance, records []*models.TransferHistoryRecord) *instance {
	inst := &instance{
		id:      id,
		records: make(map[string]*models.TransferHistoryRecord, len(records)),
		deleted: make(map[string]*models.TransferHistoryRecord),
	}
	inst.reset()
	for _, r := range records {
		if r.Deleted {
			inst.deleted[r.Hash] = r
		} else {
			inst.records[r.Hash] = r
		}
	}
	if row != nil {
		inst.firstRecordedAt = row.FirstRecordedAt
		inst.lastReadingAt = row.LastReadingAt
		inst.writes = row.Writes
		inst.resumed = true
		inst.loaded = true
		if row.CompleteReadingAt != nil {
			inst.completeAt = *row.CompleteReadingAt
		}
	}
	return inst
}

func (inst *instance) reset() {
	inst.changedRecords = make(map[*models.TransferHistoryRecord]struct{})
	inst.changedCounters = make(map[*models.TransferHistoryRecord]struct{})
	inst.moved = make(map[movedKey]*bytesMoved)
	inst.continued = make(map[*models.TransferHistoryRecord]*models.TransferHistoryRecord)
	inst.gaps = make(map[int64]int64)
	inst.hashChanges, inst.events = nil, nil
	inst.libraryDays = make(map[int64]*models.TransferHistoryLibraryDay)
	inst.instanceHours = make(map[int64]*models.TransferHistoryInstanceHour)
	inst.unwritten = false
}

// pass is one reading while apply reads it. first is set for the first
// reading of the instance, and previous is the time of the reading before.
// After a load, previous is the time of the complete reading.
// baselineOpen is set when the stored first write lacks parts, so a torrent
// without a record can be one that qBittorrent had at the first recording.
type pass struct {
	reading      *Reading
	first        bool
	baselineOpen bool
	previous     time.Time
	deletedLate  map[string]time.Time
	mapper       *trackergroup.Mapper
	hashes       map[string]struct{}
}

func (p *pass) present(hash string) bool {
	if p.hashes == nil {
		p.hashes = make(map[string]struct{}, len(p.reading.Torrents))
		for i := range p.reading.Torrents {
			p.hashes[p.reading.Torrents[i].Hash] = struct{}{}
		}
	}
	_, here := p.hashes[hash]
	return here
}

// apply adds the transfer and the events since the last reading to the
// changes. It takes each note in notes whose torrent is gone, and drops each
// note that is too old.
func (inst *instance) apply(r Reading, mapper *trackergroup.Mapper, parser *releases.Parser, notes map[string][]deleteNote) {
	p := &pass{reading: &r, first: inst.firstRecordedAt.IsZero(), previous: inst.lastReadingAt, mapper: mapper}
	if inst.loaded {
		p.previous, p.baselineOpen, p.deletedLate = inst.completeAt, inst.completeAt.IsZero(), inst.deletedLate
		inst.deletedLate = nil
	}
	if p.first {
		inst.firstRecordedAt = r.Time
	} else {
		inst.addGap(r)
	}
	inst.lastReadingAt = r.Time
	inst.resumed, inst.loaded = false, false
	inst.unwritten = true

	hour, today := hourOf(r.Time), dayOf(r.Time)
	inst.addLibrary(&r, hour, today)
	for i := range r.Torrents {
		t := &r.Torrents[i]
		rec, added, firstRecording, up, down := inst.read(t, p)
		inst.addEvents(rec, t, added, firstRecording, p.reading.Time)
		if rec.Final == nil {
			rec.Final = &models.TransferHistoryFinal{}
		}
		rec.Final.Ratio, rec.Final.SeedingSeconds = t.Ratio, t.SeedingSeconds

		// The flag of the bytes is the flag before this reading.
		crossSeed := rec.StatisticsCrossSeed != nil && *rec.StatisticsCrossSeed
		if t.Uploaded != rec.LastUploaded || t.Downloaded != rec.LastDownloaded {
			inst.changedCounters[rec] = struct{}{}
		}
		if updateRecord(rec, t, mapper, parser) || added {
			inst.changedRecords[rec] = struct{}{}
		}
		if up == 0 && down == 0 {
			continue
		}
		public := rec.Private != nil && !*rec.Private
		addTo(inst.moved, movedKey{record: rec, hour: hour, domain: rec.TrackerDomain, public: public, category: rec.Category, crossSeed: crossSeed}, up, down)
	}

	// Each torrent of the reading has a record in records now, so a larger
	// number of records shows a delete.
	if len(r.Torrents) < len(inst.records) {
		for hash, rec := range inst.records {
			if !p.present(hash) {
				inst.delete(rec, r.Time, notes)
			}
		}
	}
	dropOldNotes(notes, r.Time)
}

// addLibrary keeps the library and the state of the instance that the
// reading shows. The last reading of a day or an hour decides its values,
// except the Unique size of a day, which a reading without one keeps, and the
// speeds of an hour, which keep the highest.
func (inst *instance) addLibrary(r *Reading, hour, today int64) {
	library := inst.libraryDays[today]
	if library == nil {
		library = &models.TransferHistoryLibraryDay{Day: today}
		inst.libraryDays[today] = library
	}
	library.Torrents, library.TotalSize = int64(len(r.Torrents)), 0
	for i := range r.Torrents {
		library.TotalSize += r.Torrents[i].Size
	}
	if r.UniqueSize != nil {
		library.UniqueSize = new(*r.UniqueSize)
	}

	state := inst.instanceHours[hour]
	if state == nil {
		state = &models.TransferHistoryInstanceHour{Hour: hour}
		inst.instanceHours[hour] = state
	}
	state.States = make(map[string]int64)
	for i := range r.Torrents {
		state.States[r.Torrents[i].State]++
	}
	state.FreeDisk = r.FreeDisk
	state.MaxUploadSpeed = max(state.MaxUploadSpeed, r.MaxUploadSpeed)
	state.MaxDownloadSpeed = max(state.MaxDownloadSpeed, r.MaxDownloadSpeed)
}

// read returns the record of t, whether the reading adds the torrent, whether
// the torrent counts as one that qBittorrent had at the first recording, and
// the bytes that the torrent moved since the last reading.
func (inst *instance) read(t *Torrent, p *pass) (rec *models.TransferHistoryRecord, added, firstRecording bool, up, down int64) {
	// A torrent that qBittorrent had before the last reading, but left out of
	// it, did not move its bytes since then.
	leftOut := t.AddedAt.Before(p.previous.Add(-clockSkew))
	rec, known := inst.records[t.Hash]
	if !known {
		magnet, returned := inst.magnetOf(t, p), inst.deleted[t.Hash]
		switch {
		case magnet != nil && returned != nil:
			inst.continueIn(returned, magnet)
			rec = returned
		case magnet != nil:
			inst.changeHash(magnet, t.Hash)
			rec = magnet
		case returned != nil:
			if deletedAt, late := p.deletedLate[t.Hash]; late {
				leftOut = t.AddedAt.Before(deletedAt.Add(-clockSkew))
			}
			inst.addAgain(returned, t, p.reading.Time, leftOut)
			rec, added = returned, true
		default:
			return inst.add(t, p, leftOut)
		}
	}
	up, down = max(t.Uploaded-rec.LastUploaded, 0), max(t.Downloaded-rec.LastDownloaded, 0)
	if up > 0 && rec.UploadFirst24h != nil && p.reading.Time.Before(t.AddedAt.Add(day)) {
		rec.UploadFirst24h = new(*rec.UploadFirst24h + up)
		inst.changedRecords[rec] = struct{}{}
	}
	return rec, added, p.first, up, down
}

// add makes the record of a torrent that qui sees for the first time. Its
// bytes are transfer, unless they are in the Baseline.
func (inst *instance) add(t *Torrent, p *pass, leftOut bool) (rec *models.TransferHistoryRecord, added, firstRecording bool, up, down int64) {
	// The stored first write lacks parts, so a torrent that qBittorrent can
	// have had at the first recording goes into the Baseline.
	firstRecording = p.first || (p.baselineOpen && t.AddedAt.Before(inst.firstRecordedAt.Add(clockSkew)))
	rec = newRecord(t, p.mapper, p.reading.Time, firstRecording || leftOut)
	inst.records[t.Hash] = rec
	inst.changedCounters[rec] = struct{}{}
	if rec.BaselineUploaded == 0 && rec.BaselineDownloaded == 0 {
		up, down = t.Uploaded, t.Downloaded
	}
	return rec, true, firstRecording, up, down
}

// magnetOf returns the live record under the v1 infohash of t, when the hash
// of that record is gone from the reading. qBittorrent changes the hash of a
// magnet that it added by its v1 hash when the metadata shows a hybrid
// torrent.
func (inst *instance) magnetOf(t *Torrent, p *pass) *models.TransferHistoryRecord {
	rec, known := inst.records[t.InfohashV1]
	if !known || p.present(t.InfohashV1) {
		return nil
	}
	return rec
}

func (inst *instance) changeHash(rec *models.TransferHistoryRecord, hash string) {
	inst.hashChanges = append(inst.hashChanges, models.TransferHistoryHashChange{From: rec.Hash, To: hash})
	delete(inst.records, rec.Hash)
	rec.Hash = hash
	inst.records[hash] = rec
}

// continueIn makes returned, the deleted record of a torrent, the record of
// that torrent again, when its magnet got its hash back. The life of the
// magnet continues in returned, so the change gives no event and no bytes. The
// record of the magnet stays, as a deleted record.
func (inst *instance) continueIn(returned, magnet *models.TransferHistoryRecord) {
	returned.LastUploaded, returned.LastDownloaded = magnet.LastUploaded, magnet.LastDownloaded
	returned.Completed, returned.UploadFirst24h = magnet.Completed, magnet.UploadFirst24h
	keepBaselineFlag(returned)
	returned.StatisticsCrossSeed = magnet.StatisticsCrossSeed
	returned.Final, magnet.Final = magnet.Final, nil
	returned.Deleted, magnet.Deleted = false, true
	delete(inst.deleted, returned.Hash)
	inst.records[returned.Hash] = returned
	delete(inst.records, magnet.Hash)
	inst.deleted[magnet.Hash] = magnet
	inst.changedCounters[returned] = struct{}{}
	inst.changedRecords[returned] = struct{}{}
	inst.changedRecords[magnet] = struct{}{}
	inst.continued[magnet] = returned
}

// addEvents adds the add event and the completion event that the reading at
// now shows for t. A torrent gets one completion event in each life. A torrent
// that qBittorrent had at the first recording gives no event.
func (inst *instance) addEvents(rec *models.TransferHistoryRecord, t *Torrent, added, firstRecording bool, now time.Time) {
	if added && !firstRecording {
		inst.events = append(inst.events, models.TransferHistoryEvent{Record: rec, Kind: models.TransferHistoryEventAdd, Time: now})
	}
	if !t.Complete || rec.Completed {
		return
	}
	rec.Completed = true
	inst.changedRecords[rec] = struct{}{}
	if !firstRecording {
		inst.events = append(inst.events, models.TransferHistoryEvent{Record: rec, Kind: models.TransferHistoryEventCompletion, Time: now})
	}
}

// keepBaselineFlag stores the Statistics cross-seed flag of the first life of
// rec for its Baseline, before a new life decides the flag again.
func keepBaselineFlag(rec *models.TransferHistoryRecord) {
	if rec.BaselineStatisticsCrossSeed == nil {
		rec.BaselineStatisticsCrossSeed = new(rec.StatisticsCrossSeed != nil && *rec.StatisticsCrossSeed)
	}
}

// addAgain starts a new life of a deleted record. Its counters start at zero
// and it gets its Statistics cross-seed flag again, unless qBittorrent left
// the torrent out of the readings since the delete.
func (inst *instance) addAgain(rec *models.TransferHistoryRecord, t *Torrent, now time.Time, leftOut bool) {
	rec.Deleted = false
	delete(inst.deleted, rec.Hash)
	inst.records[rec.Hash] = rec
	if leftOut {
		return
	}
	rec.LastUploaded, rec.LastDownloaded = 0, 0
	// The stored counters are those of the last life, also when the new
	// counters are still zero.
	inst.changedCounters[rec] = struct{}{}
	rec.Completed = false
	keepBaselineFlag(rec)
	rec.StatisticsCrossSeed = nil
	rec.UploadFirst24h = nil
	if now.Before(t.AddedAt.Add(day)) {
		rec.UploadFirst24h = new(int64(0))
	}
}

// delete marks rec as deleted and adds its delete event, with the cause of
// its note in notes.
func (inst *instance) delete(rec *models.TransferHistoryRecord, now time.Time, notes map[string][]deleteNote) {
	cause := takeNote(notes, rec.Hash, now)
	event := &models.TransferHistoryDelete{
		Uploaded: rec.LastUploaded, Downloaded: rec.LastDownloaded,
		CauseKind: string(cause.Kind), CauseName: cause.Name,
	}
	if rec.Final != nil {
		event.Ratio, event.SeedingSeconds = &rec.Final.Ratio, &rec.Final.SeedingSeconds
		rec.Final = nil
	}
	inst.events = append(inst.events, models.TransferHistoryEvent{Record: rec, Kind: models.TransferHistoryEventDelete, Time: now, Delete: event})
	rec.Deleted = true
	delete(inst.records, rec.Hash)
	inst.deleted[rec.Hash] = rec
	inst.changedRecords[rec] = struct{}{}
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

// holdsDaysBefore reports whether the changes hold transfer or hourly state
// of a UTC day before day.
func (inst *instance) holdsDaysBefore(day int64) bool {
	for key := range inst.moved {
		if key.hour < day*24 {
			return true
		}
	}
	for hour := range inst.instanceHours {
		if hour < day*24 {
			return true
		}
	}
	return false
}

func (inst *instance) hasChanges() bool {
	return inst.unwritten || len(inst.changedRecords) > 0 || len(inst.changedCounters) > 0
}

// parts is one write of the changes since the last write, split into parts
// that are stored in order, each in its own transaction. part holds the index
// of the part of each torrent.
type parts struct {
	writes []*models.TransferHistoryWrite
	part   map[*models.TransferHistoryRecord]int
}

// parts splits the changes into parts of at most size torrents. Each part
// holds all changes of its torrents, and the first part also holds the
// changes of the instance. Each part takes one write number, and only the
// last one stores the time of its reading as the complete reading. The records in the parts
// are the records of inst: the writes must finish before the next apply.
func (inst *instance) parts(size int) parts {
	part := make(map[*models.TransferHistoryRecord]int, len(inst.changedRecords)+len(inst.changedCounters))
	for rec := range inst.changedRecords {
		part[rec] = 0
	}
	for rec := range inst.changedCounters {
		part[rec] = 0
	}
	for key := range inst.moved {
		part[key.record] = 0
	}
	for _, e := range inst.events {
		part[e.Record] = 0
	}
	count := max(1, (len(part)+size-1)/size)
	if count > 1 {
		// The order of the hashes makes the parts of the same changes the same.
		sorted := slices.SortedFunc(maps.Keys(part), func(a, b *models.TransferHistoryRecord) int { return strings.Compare(a.Hash, b.Hash) })
		for i, rec := range sorted {
			part[rec] = i / size
		}
		for magnet, returned := range inst.continued {
			part[magnet] = part[returned]
		}
	}

	writes := make([]*models.TransferHistoryWrite, count)
	for i := range writes {
		writes[i] = &models.TransferHistoryWrite{
			InstanceID:      inst.id,
			FirstRecordedAt: inst.firstRecordedAt,
			LastReadingAt:   inst.lastReadingAt,
			Number:          inst.writes + int64(i) + 1,
		}
		complete := inst.completeAt
		if i == count-1 {
			complete = inst.lastReadingAt
		}
		if !complete.IsZero() {
			writes[i].CompleteReadingAt = new(complete)
		}
	}
	first := writes[0]
	first.HashChanges = inst.hashChanges
	for hour, seconds := range inst.gaps {
		first.Gaps = append(first.Gaps, models.TransferHistoryGap{Hour: hour, Seconds: seconds})
	}
	for _, d := range inst.libraryDays {
		first.LibraryDays = append(first.LibraryDays, *d)
	}
	for _, h := range inst.instanceHours {
		first.InstanceHours = append(first.InstanceHours, *h)
	}

	for rec := range inst.changedRecords {
		w := writes[part[rec]]
		w.Records = append(w.Records, rec)
	}
	for rec := range inst.changedCounters {
		w := writes[part[rec]]
		w.Counters = append(w.Counters, rec)
		// A record that a write with a lost answer stored has no id yet.
		if _, listed := inst.changedRecords[rec]; !listed && rec.ID == 0 {
			w.Records = append(w.Records, rec)
		}
	}
	for _, e := range inst.events {
		w := writes[part[e.Record]]
		w.Events = append(w.Events, e)
	}

	type partTotals struct {
		torrentDays  map[torrentDay]*bytesMoved
		trackerHours map[totalKey]*bytesMoved
		categoryDays map[totalKey]*bytesMoved
	}
	sums := make([]partTotals, count)
	for i := range sums {
		sums[i] = partTotals{make(map[torrentDay]*bytesMoved), make(map[totalKey]*bytesMoved), make(map[totalKey]*bytesMoved)}
	}
	for k, b := range inst.moved {
		sum := sums[part[k.record]]
		today := floorDiv(k.hour, 24)
		addTo(sum.torrentDays, torrentDay{k.record, today}, b.uploaded, b.downloaded)
		addTo(sum.trackerHours, totalKey{bucket: k.hour, domain: k.domain, public: k.public, crossSeed: k.crossSeed}, b.uploaded, b.downloaded)
		addTo(sum.categoryDays, totalKey{bucket: today, domain: k.domain, public: k.public, category: k.category, crossSeed: k.crossSeed}, b.uploaded, b.downloaded)
	}
	for i, sum := range sums {
		w := writes[i]
		for k, b := range sum.torrentDays {
			w.TorrentDays = append(w.TorrentDays, models.TransferHistoryTorrentDay{Record: k.record, Day: k.day, Uploaded: b.uploaded, Downloaded: b.downloaded})
		}
		w.TrackerHours = totals(sum.trackerHours)
		w.CategoryDays = totals(sum.categoryDays)
	}
	return parts{writes: writes, part: part}
}

type torrentDay struct {
	record *models.TransferHistoryRecord
	day    int64
}

// forget drops the changes of the first stored parts of p, which a write
// stored. It drops all changes when the write stored each part.
func (inst *instance) forget(p parts, stored int) {
	inst.writes += int64(stored)
	if stored == len(p.writes) {
		inst.completeAt = inst.lastReadingAt
		inst.reset()
		return
	}
	if stored == 0 {
		return
	}
	isStored := func(rec *models.TransferHistoryRecord) bool { return p.part[rec] < stored }
	maps.DeleteFunc(inst.changedRecords, func(rec *models.TransferHistoryRecord, _ struct{}) bool { return isStored(rec) })
	maps.DeleteFunc(inst.changedCounters, func(rec *models.TransferHistoryRecord, _ struct{}) bool { return isStored(rec) })
	maps.DeleteFunc(inst.moved, func(k movedKey, _ *bytesMoved) bool { return isStored(k.record) })
	maps.DeleteFunc(inst.continued, func(magnet, _ *models.TransferHistoryRecord) bool { return isStored(magnet) })
	inst.events = slices.DeleteFunc(inst.events, func(e models.TransferHistoryEvent) bool { return isStored(e.Record) })
	// The first part holds the changes of the instance.
	inst.hashChanges = nil
	clear(inst.gaps)
	clear(inst.libraryDays)
	clear(inst.instanceHours)
	inst.unwritten = false
}

// unsure is what a part whose outcome qui does not know can have stored.
type unsure int

const (
	sure unsure = iota
	// sameRows is a part that holds no rows that add up, so a second write of
	// it stores the same rows.
	sameRows
	// addedRows is a part whose rows would count twice in a second write.
	addedRows
)

// markUnsure notes that the database can hold w, a part whose outcome qui
// does not know.
func (inst *instance) markUnsure(w *models.TransferHistoryWrite) {
	if len(w.TorrentDays)+len(w.TrackerHours)+len(w.CategoryDays)+len(w.Gaps)+len(w.Events) > 0 {
		inst.unsure = addedRows
	} else if inst.unsure == sure {
		inst.unsure = sameRows
	}
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

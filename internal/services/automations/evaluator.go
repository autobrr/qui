// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/pkg/releases"
	"github.com/autobrr/qui/pkg/stringutils"
)

const maxConditionDepth = 20

const minContainsNameLength = 10

type CategoryEntry struct {
	Hash           string
	Name           string
	NormalizedName string
}

type EvalContext struct {
	torrentsByHash map[string]*qbt.Torrent
	hardlinkIndex  *HardlinkIndex
	crossSeeds     *crossSeedGroups
	moveChecks     map[moveCheckKey]checkedMove

	trackers                   qbittorrent.TrackerLookup
	TrackerHealth              *qbittorrent.TrackerHealthSnapshot
	HardlinkScopeByHash        map[string]string
	HardlinkCrossScopeByHash   map[string]string
	HasMissingFilesByHash      map[string]bool
	HasSkippedFilesByHash      map[string]bool
	InstanceCanRead            bool
	InstanceHasTrustedIdentity bool
	freeSpaceLedgers           map[int]*freeSpaceLedger
	activeFreeSpace            *freeSpaceLedger
	CrossSeedFilesByHash       map[string]qbt.TorrentFiles
	HardlinkSignatureByHash    map[string]string

	CategoryIndex map[string]map[string]map[string]struct{}

	CategoryNames map[string][]CategoryEntry

	NowUnix int64

	CrossInstanceHashSet        map[string]struct{}
	CrossInstanceSeedingHashSet map[string]struct{}

	SameInstanceCrossSeedHashSet        map[string]struct{}
	SameInstanceCrossSeedSeedingHashSet map[string]struct{}
	SameInstanceCrossSeedTagsByHash     map[string][]string
	SameInstanceContentByHash           map[string][]string
	SameInstanceCrossSeedUnknown        map[string]struct{}
	CrossInstanceUnknown                map[string]struct{}
	CrossInstanceSharedHashSet          map[string]struct{}
	CrossInstanceSharedUnknown          map[string]struct{}
	OfflineLocalInstances               []string

	SeasonPackSet            map[string]struct{}
	SeasonPackSetAnyInstance map[string]struct{}

	TrackerDisplayNameByDomain map[string]string

	CategorySavePaths map[string]string

	ReleaseParser *releases.Parser

	ActiveRuleID int

	activeGroupIndex *groupIndex

	groupIndexCache           map[int]map[string]*groupIndex
	groupConditionUsageByRule map[int]groupingConditionUsage
}

var separatorReplacer = strings.NewReplacer(".", " ", "_", " ", "-", " ")

var whitespaceCollapser = regexp.MustCompile(`\s+`)

func normalizeName(s string) string {
	s = stringutils.NormalizeUnicode(normalizeLower(s))
	s = normalizeLower(s)
	s = separatorReplacer.Replace(s)
	s = whitespaceCollapser.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

func BuildCategoryIndex(torrents []*qbt.Torrent) (map[string]map[string]map[string]struct{}, map[string][]CategoryEntry) {
	categoryIndex := make(map[string]map[string]map[string]struct{})
	categoryNames := make(map[string][]CategoryEntry)

	for _, t := range torrents {
		catKey := normalizeLowerTrim(t.Category)
		nameLower := normalizeLower(t.Name)

		if categoryIndex[catKey] == nil {
			categoryIndex[catKey] = make(map[string]map[string]struct{})
		}
		if categoryIndex[catKey][nameLower] == nil {
			categoryIndex[catKey][nameLower] = make(map[string]struct{})
		}
		categoryIndex[catKey][nameLower][t.Hash] = struct{}{}

		categoryNames[catKey] = append(categoryNames[catKey], CategoryEntry{
			Hash:           t.Hash,
			Name:           nameLower,
			NormalizedName: normalizeName(t.Name),
		})
	}

	return categoryIndex, categoryNames
}

func existsInCategory(torrentHash, torrentName, targetCategory string, ctx *EvalContext) bool {
	if ctx == nil || ctx.CategoryIndex == nil {
		return false
	}

	catKey := normalizeLowerTrim(targetCategory)
	if targetCategory != "" && catKey == "" {
		return false
	}
	nameLower := normalizeLower(torrentName)

	nameMap, ok := ctx.CategoryIndex[catKey]
	if !ok {
		return false
	}

	hashSet, ok := nameMap[nameLower]
	if !ok {
		return false
	}

	for hash := range hashSet {
		if hash != torrentHash {
			return true
		}
	}
	return false
}

func containsInCategory(torrentHash, torrentName, targetCategory string, ctx *EvalContext) bool {
	if ctx == nil || ctx.CategoryNames == nil {
		return false
	}

	catKey := normalizeLowerTrim(targetCategory)
	if targetCategory != "" && catKey == "" {
		return false
	}

	normalizedCurrent := normalizeName(torrentName)
	if len(normalizedCurrent) < minContainsNameLength {
		return false
	}

	entries, ok := ctx.CategoryNames[catKey]
	if !ok {
		return false
	}

	for _, entry := range entries {
		if entry.Hash == torrentHash {
			continue
		}
		if len(entry.NormalizedName) < minContainsNameLength {
			continue
		}
		if strings.Contains(normalizedCurrent, entry.NormalizedName) ||
			strings.Contains(entry.NormalizedName, normalizedCurrent) {
			return true
		}
	}
	return false
}

func ConditionUsesField(cond *RuleCondition, field ConditionField) bool {
	if cond == nil {
		return false
	}
	if cond.Field == field {
		return true
	}
	for _, child := range cond.Conditions {
		if ConditionUsesField(child, field) {
			return true
		}
	}
	return false
}

func evaluateTime(ctx *EvalContext) time.Time {
	if ctx != nil && ctx.NowUnix > 0 {
		return time.Unix(ctx.NowUnix, 0)
	}
	return time.Now()
}

func EvaluateConditionWithContext(cond *RuleCondition, torrent qbt.Torrent, ctx *EvalContext, depth int) bool {
	matched, known := evaluateCondition(cond, torrent, ctx, depth)
	return known && matched
}

func evaluateCondition(cond *RuleCondition, torrent qbt.Torrent, ctx *EvalContext, depth int) (matched, known bool) {
	if cond == nil || depth > maxConditionDepth {
		return false, true
	}

	if cond.Operator != OperatorExistsIn && cond.Operator != OperatorContainsIn {
		if cond.Regex || cond.Operator == OperatorMatches {
			if err := cond.CompileRegex(); err != nil {
				log.Debug().
					Err(err).
					Str("field", string(cond.Field)).
					Str("pattern", cond.Value).
					Msg("automations: regex compilation failed")
				return false, true
			}
		}
	}

	var result bool
	known = true

	if cond.IsGroup() {
		switch cond.Operator {
		case OperatorOr:
			result = false
			for _, child := range cond.Conditions {
				childMatch, childKnown := evaluateCondition(child, torrent, ctx, depth+1)
				if !childKnown {
					known = false
					continue
				}
				if childMatch {
					result = true
					known = true
					break
				}
			}
		case OperatorAnd:
			result = true
			for _, child := range cond.Conditions {
				childMatch, childKnown := evaluateCondition(child, torrent, ctx, depth+1)
				known = known && childKnown
				result = result && childMatch
			}
		default:
			result = false
		}
	} else {
		if !conditionDataKnown(cond.Field, torrent.Hash, ctx) || fieldHasNoValue(cond.Field, &torrent) {
			return false, false
		}
		result = evaluateLeaf(cond, torrent, ctx)
	}

	if !known {
		return false, false
	}

	if cond.Negate {
		result = !result
	}

	return result, true
}

func (ctx *EvalContext) setFilesystemAccess(instance *models.Instance) {
	capabilities := models.FilesystemCapabilitiesOf(instance)
	ctx.InstanceCanRead = capabilities.Read
	ctx.InstanceHasTrustedIdentity = capabilities.TrustedIdentity
}

func evaluateLeaf(cond *RuleCondition, torrent qbt.Torrent, ctx *EvalContext) bool {
	switch cond.Field {
	case FieldName:
		if cond.Operator == OperatorExistsIn {
			return existsInCategory(torrent.Hash, torrent.Name, cond.Value, ctx)
		}
		if cond.Operator == OperatorContainsIn {
			return containsInCategory(torrent.Hash, torrent.Name, cond.Value, ctx)
		}
		return compareString(torrent.Name, cond)
	case FieldHash:
		return compareString(torrent.Hash, cond)
	case FieldInfohashV1:
		return compareString(torrent.InfohashV1, cond)
	case FieldInfohashV2:
		return compareString(torrent.InfohashV2, cond)
	case FieldMagnetURI:
		return compareString(torrent.MagnetURI, cond)
	case FieldCategory:
		return compareString(torrent.Category, cond)
	case FieldTags:
		return compareTags(torrent.Tags, cond)
	case FieldSavePath:
		return compareString(torrent.SavePath, cond)
	case FieldContentPath:
		return compareString(torrent.ContentPath, cond)
	case FieldDownloadPath:
		return compareString(torrent.DownloadPath, cond)
	case FieldCreatedBy:
		return compareString(torrent.CreatedBy, cond)
	case FieldContentType:
		return compareString(torrentContentType(torrent, ctx), cond)
	case FieldEffectiveName:
		return compareString(torrentEffectiveName(torrent, ctx), cond)
	case FieldRlsSource:
		return compareString(torrentRlsSource(torrent, ctx), cond)
	case FieldRlsResolution:
		return compareString(torrentRlsResolution(torrent, ctx), cond)
	case FieldRlsCodec:
		return compareString(torrentRlsCodec(torrent, ctx), cond)
	case FieldRlsHDR:
		return compareString(torrentRlsHDR(torrent, ctx), cond)
	case FieldRlsAudio:
		return compareString(torrentRlsAudio(torrent, ctx), cond)
	case FieldRlsChannels:
		return compareString(torrentRlsChannels(torrent, ctx), cond)
	case FieldRlsGroup:
		return compareString(torrentRlsGroup(torrent, ctx), cond)
	case FieldRlsYear:
		return compareRlsYearIfSet(torrentRlsYear(torrent, ctx), cond)
	case FieldState:
		return compareState(torrent, cond, ctx)
	case FieldTracker, FieldTrackers:
		return compareTrackers(torrent, cond, ctx)
	case FieldTrackerStatus:
		return compareTrackerStatuses(torrent, cond, ctx)
	case FieldTrackerMessage:
		return compareTrackerMessages(torrent, cond, ctx)
	case FieldComment:
		return compareString(torrent.Comment, cond)

	case FieldSize:
		return compareInt64(torrent.Size, cond)
	case FieldTotalSize:
		return compareInt64(torrent.TotalSize, cond)
	case FieldCompleted:
		return compareInt64(torrent.Completed, cond)
	case FieldDownloaded:
		return compareInt64(torrent.Downloaded, cond)
	case FieldDownloadedSession:
		return compareInt64(torrent.DownloadedSession, cond)
	case FieldUploaded:
		return compareInt64(torrent.Uploaded, cond)
	case FieldUploadedSession:
		return compareInt64(torrent.UploadedSession, cond)
	case FieldAmountLeft:
		return compareInt64(torrent.AmountLeft, cond)
	case FieldFreeSpace:
		if ctx == nil || ctx.activeFreeSpace == nil {
			return false
		}
		return compareInt64(ctx.activeFreeSpace.projectedFreeSpace(), cond)

	case FieldAddedOn:
		return compareAgeIfSet(torrent.AddedOn, cond, ctx)
	case FieldCompletionOn:
		return compareAgeIfSet(qbittorrent.NormalizeCompletionTimestamp(torrent.CompletionOn), cond, ctx)
	case FieldLastActivity:
		return compareAgeIfSet(torrent.LastActivity, cond, ctx)
	case FieldSeenComplete:
		return compareAgeIfSet(qbittorrent.NormalizeCompletionTimestamp(torrent.SeenComplete), cond, ctx)
	case FieldETA:
		return compareInt64(torrent.ETA, cond)
	case FieldReannounce:
		return compareInt64(torrent.Reannounce, cond)
	case FieldSeedingTime:
		return compareInt64(torrent.SeedingTime, cond)
	case FieldTimeActive:
		return compareInt64(torrent.TimeActive, cond)
	case FieldMaxSeedingTime:
		return compareInt64(torrent.MaxSeedingTime, cond)
	case FieldMaxInactiveSeedingTime:
		return compareInt64(torrent.MaxInactiveSeedingTime, cond)
	case FieldSeedingTimeLimit:
		return compareInt64(torrent.SeedingTimeLimit, cond)
	case FieldInactiveSeedingTimeLimit:
		return compareInt64(torrent.InactiveSeedingTimeLimit, cond)

	case FieldAddedOnAge:
		return compareAgeIfSet(torrent.AddedOn, cond, ctx)
	case FieldCompletionOnAge:
		return compareAgeIfSet(qbittorrent.NormalizeCompletionTimestamp(torrent.CompletionOn), cond, ctx)
	case FieldLastActivityAge:
		return compareAgeIfSet(torrent.LastActivity, cond, ctx)

	case FieldRatio:
		return compareFloat64(torrent.Ratio, cond)
	case FieldRatioLimit:
		return compareFloat64(torrent.RatioLimit, cond)
	case FieldMaxRatio:
		return compareFloat64(torrent.MaxRatio, cond)
	case FieldUploadedOverSize:
		if torrent.TotalSize == 0 {
			return false
		}
		return compareFloat64(float64(torrent.Uploaded)/float64(torrent.TotalSize), cond)
	case FieldProgress:
		return compareFloat64(torrent.Progress, normalizeProgressCondition(cond))
	case FieldAvailability:
		return compareFloat64(torrent.Availability, cond)
	case FieldPopularity:
		return compareFloat64(torrent.Popularity, cond)

	case FieldDlSpeed:
		return compareInt64(torrent.DlSpeed, cond)
	case FieldUpSpeed:
		return compareInt64(torrent.UpSpeed, cond)
	case FieldDlLimit:
		return compareInt64(torrent.DlLimit, cond)
	case FieldUpLimit:
		return compareInt64(torrent.UpLimit, cond)

	case FieldNumSeeds:
		return compareInt64(torrent.NumSeeds, cond)
	case FieldNumLeechs:
		return compareInt64(torrent.NumLeechs, cond)
	case FieldNumComplete:
		return compareInt64(torrent.NumComplete, cond)
	case FieldNumIncomplete:
		return compareInt64(torrent.NumIncomplete, cond)
	case FieldTrackersCount:
		return compareInt64(torrent.TrackersCount, cond)
	case FieldPriority:
		return compareInt64(torrent.Priority, cond)
	case FieldGroupSize:
		size := int64(0)
		if idx := resolveConditionGroupIndex(cond, ctx); idx != nil {
			size = int64(idx.SizeForHash(torrent.Hash))
		}
		return compareInt64(size, cond)

	case models.FieldSystemHour:
		return compareInt64(int64(evaluateTime(ctx).Hour()), cond)
	case models.FieldSystemMinute:
		return compareInt64(int64(evaluateTime(ctx).Minute()), cond)
	case models.FieldSystemDayOfWeek:
		return compareInt64(int64(evaluateTime(ctx).Weekday()), cond)
	case models.FieldSystemDay:
		return compareInt64(int64(evaluateTime(ctx).Day()), cond)
	case models.FieldSystemMonth:
		return compareInt64(int64(evaluateTime(ctx).Month()), cond)
	case models.FieldSystemYear:
		return compareInt64(int64(evaluateTime(ctx).Year()), cond)

	case FieldPrivate:
		return compareBool(torrent.Private, cond)
	case FieldAutoManaged:
		return compareBool(torrent.AutoManaged, cond)
	case FieldFirstLastPiecePrio:
		return compareBool(torrent.FirstLastPiecePrio, cond)
	case FieldForceStart:
		return compareBool(torrent.ForceStart, cond)
	case FieldSequentialDownload:
		return compareBool(torrent.SequentialDownload, cond)
	case FieldSuperSeeding:
		return compareBool(torrent.SuperSeeding, cond)
	case FieldIsUnregistered:
		return compareBool(ctx != nil && ctx.TrackerHealth.Health(torrent.Hash) == qbittorrent.TrackerHealthUnregistered, cond)

	case FieldHardlinkScope:
		return compareHardlinkScope(ctx.HardlinkScopeByHash[torrent.Hash], cond)

	case FieldHardlinkScopeCross:
		return compareHardlinkScope(ctx.HardlinkCrossScopeByHash[torrent.Hash], cond)

	case FieldHasMissingFiles:
		return compareBool(ctx.HasMissingFilesByHash[torrent.Hash], cond)

	case FieldHasSkippedFiles:
		if ctx == nil {
			return false
		}
		hasSkipped, ok := ctx.HasSkippedFilesByHash[torrent.Hash]
		if !ok {
			return false
		}
		return compareBool(hasSkipped, cond)

	case FieldIsGrouped:
		grouped := false
		if idx := resolveConditionGroupIndex(cond, ctx); idx != nil {
			grouped = idx.SizeForHash(torrent.Hash) > 1
		}
		return compareBool(grouped, cond)

	case FieldExistsOnOtherInstance:
		exists := false
		if ctx != nil && ctx.CrossInstanceHashSet != nil {
			_, exists = ctx.CrossInstanceHashSet[torrent.Hash]
		}
		return compareBool(exists, cond)

	case FieldSeedingOnOtherInstance:
		seeding := false
		if ctx != nil && ctx.CrossInstanceSeedingHashSet != nil {
			_, seeding = ctx.CrossInstanceSeedingHashSet[torrent.Hash]
		}
		return compareBool(seeding, cond)

	case FieldExistsOnSameInstance:
		exists := false
		if ctx != nil && ctx.SameInstanceCrossSeedHashSet != nil {
			_, exists = ctx.SameInstanceCrossSeedHashSet[torrent.Hash]
		}
		return compareBool(exists, cond)

	case FieldSeedingOnSameInstance:
		seeding := false
		if ctx != nil && ctx.SameInstanceCrossSeedSeedingHashSet != nil {
			_, seeding = ctx.SameInstanceCrossSeedSeedingHashSet[torrent.Hash]
		}
		return compareBool(seeding, cond)

	case FieldCrossSeedTags:
		parts := make([]string, 0, 2)
		if torrent.Tags != "" {
			parts = append(parts, torrent.Tags)
		}
		if ctx != nil {
			parts = append(parts, ctx.SameInstanceCrossSeedTagsByHash[torrent.Hash]...)
		}
		return compareTags(strings.Join(parts, ","), cond)

	case FieldSeasonPackStatus:
		return compareString(seasonPackStatus(parsedTorrentRelease(torrent, ctx), ctx.SeasonPackSet), cond)
	case FieldSeasonPackStatusAnyInstance:
		return compareString(seasonPackStatus(parsedTorrentRelease(torrent, ctx), ctx.SeasonPackSetAnyInstance), cond)
	}
	return false
}

func compareState(torrent qbt.Torrent, cond *RuleCondition, ctx *EvalContext) bool {
	if cond == nil {
		return false
	}

	matches := matchesStateValue(torrent, cond.Value, ctx)
	switch cond.Operator {
	case OperatorEqual:
		return matches
	case OperatorNotEqual:
		return !matches
	default:
		return compareString(string(torrent.State), cond)
	}
}

func matchesStateValue(torrent qbt.Torrent, value string, ctx *EvalContext) bool {
	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return false
	}

	switch strings.ToLower(normalized) {
	case "completed":
		return torrent.Progress >= 1.0
	case "downloading":
		return slices.Contains([]qbt.TorrentState{
			qbt.TorrentStateDownloading,
			qbt.TorrentStateStalledDl,
			qbt.TorrentStateMetaDl,
			qbt.TorrentStateForcedMetaDl,
			qbt.TorrentStateQueuedDl,
			qbt.TorrentStateAllocating,
			qbt.TorrentStateCheckingDl,
			qbt.TorrentStateForcedDl,
		}, torrent.State)
	case "uploading", "seeding":
		return slices.Contains([]qbt.TorrentState{
			qbt.TorrentStateUploading,
			qbt.TorrentStateStalledUp,
			qbt.TorrentStateQueuedUp,
			qbt.TorrentStateCheckingUp,
			qbt.TorrentStateForcedUp,
		}, torrent.State)
	case "paused", "stopped":
		return slices.Contains([]qbt.TorrentState{
			qbt.TorrentStatePausedDl,
			qbt.TorrentStatePausedUp,
			qbt.TorrentStateStoppedDl,
			qbt.TorrentStateStoppedUp,
		}, torrent.State)
	case "running", "resumed":
		return !slices.Contains([]qbt.TorrentState{
			qbt.TorrentStatePausedDl,
			qbt.TorrentStatePausedUp,
			qbt.TorrentStateStoppedDl,
			qbt.TorrentStateStoppedUp,
		}, torrent.State)
	case "active":
		return slices.Contains([]qbt.TorrentState{
			qbt.TorrentStateDownloading,
			qbt.TorrentStateUploading,
			qbt.TorrentStateForcedDl,
			qbt.TorrentStateForcedUp,
		}, torrent.State)
	case "inactive":
		return !slices.Contains([]qbt.TorrentState{
			qbt.TorrentStateDownloading,
			qbt.TorrentStateUploading,
			qbt.TorrentStateForcedDl,
			qbt.TorrentStateForcedUp,
		}, torrent.State)
	case "stalled":
		return slices.Contains([]qbt.TorrentState{
			qbt.TorrentStateStalledDl,
			qbt.TorrentStateStalledUp,
		}, torrent.State)
	case "stalled_uploading", "stalled_seeding":
		return torrent.State == qbt.TorrentStateStalledUp
	case "stalled_downloading":
		return torrent.State == qbt.TorrentStateStalledDl
	case "checking":
		return slices.Contains([]qbt.TorrentState{
			qbt.TorrentStateCheckingDl,
			qbt.TorrentStateCheckingUp,
			qbt.TorrentStateCheckingResumeData,
		}, torrent.State)
	case "moving":
		return torrent.State == qbt.TorrentStateMoving
	case "errored", "error":
		return torrent.State == qbt.TorrentStateError || torrent.State == qbt.TorrentStateMissingFiles
	case "missingfiles":
		return torrent.State == qbt.TorrentStateMissingFiles
	case "unregistered", "tracker_down", "tracker_error":
		return ctx != nil && ctx.TrackerHealth.Health(torrent.Hash) == qbittorrent.TrackerHealth(strings.ToLower(normalized))
	}

	return strings.EqualFold(string(torrent.State), normalized)
}

func compareString(value string, cond *RuleCondition) bool {
	if cond.Regex || cond.Operator == OperatorMatches {
		if cond.Compiled == nil {
			return false
		}
		matched := cond.Compiled.MatchString(value)
		if cond.Operator == OperatorNotContains || cond.Operator == OperatorNotEqual {
			return !matched
		}
		return matched
	}

	switch cond.Operator {
	case OperatorEqual:
		return strings.EqualFold(value, cond.Value)
	case OperatorNotEqual:
		return !strings.EqualFold(value, cond.Value)
	case OperatorContains:
		return stringutils.ContainsFold(value, cond.Value)
	case OperatorNotContains:
		return !stringutils.ContainsFold(value, cond.Value)
	case OperatorStartsWith:
		return stringutils.HasPrefixFold(value, cond.Value)
	case OperatorEndsWith:
		return stringutils.HasSuffixFold(value, cond.Value)
	default:
		return false
	}
}

func compareTrackers(torrent qbt.Torrent, cond *RuleCondition, ctx *EvalContext) bool {
	trackers := ctx.trackersOf(torrent.Hash)
	candidates := make([]string, 0, len(trackers)*3+3)
	candidates = append(candidates, trackerCandidates(torrent.Tracker, ctx)...)
	for _, tracker := range trackers {
		candidates = append(candidates, trackerCandidates(tracker.Url, ctx)...)
	}
	return compareStringCandidates(candidates, cond)
}

func compareTrackerStatuses(torrent qbt.Torrent, cond *RuleCondition, ctx *EvalContext) bool {
	for _, tracker := range ctx.trackersOf(torrent.Hash) {
		if qbittorrent.IsPseudoTrackerLabel(tracker.Url) {
			continue
		}
		if compareTrackerStatus(int(tracker.Status), cond) {
			return true
		}
	}
	return false
}

func compareTrackerStatus(status int, cond *RuleCondition) bool {
	if cond == nil {
		return false
	}

	matches := matchesTrackerStatusValue(status, cond.Value)
	switch cond.Operator {
	case OperatorEqual:
		return matches
	case OperatorNotEqual:
		return !matches
	default:
		return false
	}
}

func matchesTrackerStatusValue(status int, value string) bool {
	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return false
	}

	if n, err := strconv.Atoi(normalized); err == nil {
		return status == n
	}

	switch strings.ToLower(normalized) {
	case "not_contacted", "notcontacted", "not contacted":
		return status == int(qbt.TrackerStatusNotContacted)
	case "working", "ok":
		return status == int(qbt.TrackerStatusOK)
	case "updating":
		return status == int(qbt.TrackerStatusUpdating)
	case "error", "not_working", "not working":
		return status == int(qbt.TrackerStatusNotWorking)
	case "tracker_error", "tracker error":
		return status == int(qbt.TrackerStatusTrackerError)
	case "unreachable":
		return status == int(qbt.TrackerStatusUnreachable)
	}

	return false
}

func compareTrackerMessages(torrent qbt.Torrent, cond *RuleCondition, ctx *EvalContext) bool {
	for _, tracker := range ctx.trackersOf(torrent.Hash) {
		if qbittorrent.IsPseudoTrackerLabel(tracker.Url) {
			continue
		}
		if compareTrackerMessage(tracker.Message, cond) {
			return true
		}
	}
	return false
}

func compareTrackerMessage(message string, cond *RuleCondition) bool {
	if cond == nil {
		return false
	}

	if strings.EqualFold(strings.TrimSpace(cond.Value), "nil") {
		switch cond.Operator {
		case OperatorEqual:
			return message == ""
		case OperatorNotEqual:
			return message != ""
		default:
			return false
		}
	}

	return compareString(message, cond)
}

func trackerCandidates(trackerURL string, ctx *EvalContext) []string {
	raw := strings.TrimSpace(trackerURL)
	if qbittorrent.IsPseudoTrackerLabel(raw) {
		return nil
	}
	domain := qbittorrent.ExtractDomainFromURL(raw)
	if domain == "Unknown" {
		domain = ""
	}
	displayName := ""
	if ctx != nil && ctx.TrackerDisplayNameByDomain != nil && domain != "" {
		if name, ok := ctx.TrackerDisplayNameByDomain[strings.ToLower(domain)]; ok {
			displayName = strings.TrimSpace(name)
		}
	}

	candidates := make([]string, 0, 3)
	if raw != "" {
		candidates = append(candidates, raw)
	}
	if domain != "" && !strings.EqualFold(domain, raw) {
		candidates = append(candidates, domain)
	}
	if displayName != "" && !strings.EqualFold(displayName, domain) && !strings.EqualFold(displayName, raw) {
		candidates = append(candidates, displayName)
	}

	return candidates
}

func compareStringCandidates(candidates []string, cond *RuleCondition) bool {
	if len(candidates) == 0 {
		return compareString("", cond)
	}

	seen := make(map[string]struct{}, len(candidates))
	uniq := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		key := strings.ToLower(candidate)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		uniq = append(uniq, candidate)
	}

	if cond.Regex || cond.Operator == OperatorMatches {
		if cond.Compiled == nil {
			return false
		}
		anyMatch := slices.ContainsFunc(uniq, func(c string) bool {
			return cond.Compiled.MatchString(c)
		})
		if cond.Operator == OperatorNotContains || cond.Operator == OperatorNotEqual {
			return !anyMatch
		}
		return anyMatch
	}

	if cond.Operator == OperatorNotEqual {
		return !slices.ContainsFunc(uniq, func(c string) bool {
			return strings.EqualFold(c, cond.Value)
		})
	}
	if cond.Operator == OperatorNotContains {
		return !slices.ContainsFunc(uniq, func(c string) bool {
			return stringutils.ContainsFold(c, cond.Value)
		})
	}

	return slices.ContainsFunc(uniq, func(c string) bool {
		return compareString(c, cond)
	})
}

func compareTags(tagsRaw string, cond *RuleCondition) bool {
	if cond.Regex || cond.Operator == OperatorMatches {
		if cond.Compiled == nil {
			return false
		}
		matched := cond.Compiled.MatchString(tagsRaw)
		if cond.Operator == OperatorNotContains || cond.Operator == OperatorNotEqual {
			return !matched
		}
		return matched
	}

	tags := splitTags(tagsRaw)
	condValue := normalizeLowerTrim(cond.Value)

	switch cond.Operator {
	case OperatorEqual:
		return anyTagMatches(tags, condValue, strings.EqualFold)
	case OperatorNotEqual:
		return !anyTagMatches(tags, condValue, strings.EqualFold)
	case OperatorContains:
		return anyTagMatches(tags, condValue, stringutils.ContainsFold)
	case OperatorNotContains:
		return !anyTagMatches(tags, condValue, stringutils.ContainsFold)
	case OperatorStartsWith:
		return anyTagMatches(tags, condValue, stringutils.HasPrefixFold)
	case OperatorEndsWith:
		return anyTagMatches(tags, condValue, stringutils.HasSuffixFold)
	default:
		return false
	}
}

func anyTagMatches(tags []string, condValue string, match func(string, string) bool) bool {
	for _, tag := range tags {
		if match(tag, condValue) {
			return true
		}
	}
	return false
}

func compareInt64(value int64, cond *RuleCondition) bool {
	condValue, err := strconv.ParseInt(strings.TrimSpace(cond.Value), 10, 64)
	if err != nil && cond.Value != "" {
		return false
	}

	switch cond.Operator {
	case OperatorEqual:
		return value == condValue
	case OperatorNotEqual:
		return value != condValue
	case OperatorGreaterThan:
		return value > condValue
	case OperatorGreaterThanOrEqual:
		return value >= condValue
	case OperatorLessThan:
		return value < condValue
	case OperatorLessThanOrEqual:
		return value <= condValue
	case OperatorBetween:
		return matchesBetween(float64(value), cond)
	default:
		return false
	}
}

func matchesBetween(value float64, cond *RuleCondition) bool {
	if cond.MinValue == nil || cond.MaxValue == nil {
		return false
	}
	if *cond.MinValue > *cond.MaxValue && cond.Field.WrapsBetween() {
		return value >= *cond.MinValue || value < *cond.MaxValue
	}
	return value >= *cond.MinValue && value <= *cond.MaxValue
}

func compareFloat64(value float64, cond *RuleCondition) bool {
	condValue, err := strconv.ParseFloat(strings.TrimSpace(cond.Value), 64)
	if err != nil && cond.Value != "" {
		return false
	}

	switch cond.Operator {
	case OperatorEqual:
		return value == condValue
	case OperatorNotEqual:
		return value != condValue
	case OperatorGreaterThan:
		return value > condValue
	case OperatorGreaterThanOrEqual:
		return value >= condValue
	case OperatorLessThan:
		return value < condValue
	case OperatorLessThanOrEqual:
		return value <= condValue
	case OperatorBetween:
		return matchesBetween(value, cond)
	default:
		return false
	}
}

func normalizeProgressCondition(cond *RuleCondition) *RuleCondition {
	if cond == nil {
		return nil
	}

	normalized := *cond

	if normalized.Value != "" {
		if v, err := strconv.ParseFloat(normalized.Value, 64); err == nil {
			v = normalizeProgressValue(v)
			normalized.Value = strconv.FormatFloat(v, 'f', -1, 64)
		}
	}

	if normalized.MinValue != nil {
		v := normalizeProgressValue(*normalized.MinValue)
		normalized.MinValue = &v
	}

	if normalized.MaxValue != nil {
		v := normalizeProgressValue(*normalized.MaxValue)
		normalized.MaxValue = &v
	}

	return &normalized
}

func normalizeProgressValue(v float64) float64 {
	if v > 1 {
		v /= 100
	}

	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func compareBool(value bool, cond *RuleCondition) bool {
	condValue := strings.ToLower(cond.Value) == "true" || cond.Value == "1"

	switch cond.Operator {
	case OperatorEqual:
		return value == condValue
	case OperatorNotEqual:
		return value != condValue
	default:
		return false
	}
}

func compareHardlinkScope(value string, cond *RuleCondition) bool {
	switch cond.Operator {
	case OperatorEqual:
		return hardlinkScopeMatches(value, cond.Value)
	case OperatorNotEqual:
		return !hardlinkScopeMatches(value, cond.Value)
	default:
		return false
	}
}

func hardlinkScopeMatches(value, condValue string) bool {
	switch {
	case strings.EqualFold(condValue, HardlinkScopeInsideQBitTorrent):
		return value == HardlinkScopeTorrentsOnly || value == HardlinkScopeBoth
	case strings.EqualFold(condValue, HardlinkScopeOutsideQBitTorrent):
		return value == HardlinkScopeOutsideQBitTorrent || value == HardlinkScopeBoth
	default:
		return strings.EqualFold(value, condValue)
	}
}

func compareAge(timestamp int64, cond *RuleCondition, ctx *EvalContext) bool {
	nowUnix := time.Now().Unix()
	if ctx != nil && ctx.NowUnix > 0 {
		nowUnix = ctx.NowUnix
	}

	ageSeconds := max(nowUnix-timestamp, 0)

	return compareInt64(ageSeconds, cond)
}

func compareAgeIfSet(timestamp int64, cond *RuleCondition, ctx *EvalContext) bool {
	if timestamp <= 0 {
		return false
	}
	return compareAge(timestamp, cond, ctx)
}

func compareRlsYearIfSet(year int64, cond *RuleCondition) bool {
	if year <= 0 {
		return false
	}
	return compareInt64(year, cond)
}

func splitTags(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func (ctx *EvalContext) trackersOf(hash string) []qbt.TorrentTracker {
	if ctx == nil {
		return nil
	}
	return ctx.trackers.Trackers(hash)
}

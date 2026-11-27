// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"fmt"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/testutil/biglib"
	"github.com/autobrr/qui/internal/testutil/logbudget"
)

const packBigShow = "Lone.Harbor.S02.1080p.WEB-DL.DDP5.1.H.264-GRP7"

func packBudgetCases(tb testing.TB) map[string]*SeasonPackCheckRequest {
	tb.Helper()
	return map[string]*SeasonPackCheckRequest{
		"nothing held": {TorrentName: packNotHeld},
		"held show":    {TorrentName: packBigShow},
	}
}

func (l *bigHeldLibrary) checksPacks(svc *Service) *Service {
	l.instance.HasLocalFilesystemAccess, l.instance.UseHardlinks, l.instance.HardlinkBaseDir = true, true, "/links"
	svc.automationSettingsLoader = defaultSettings(true, 0.75)
	return svc
}

func checkSeasonPack(tb testing.TB, svc *Service, req *SeasonPackCheckRequest) {
	tb.Helper()
	resp, err := svc.CheckSeasonPackWebhook(tb.Context(), req)
	if err != nil || resp.Ready != (req.TorrentName == packBigShow) {
		tb.Fatalf("season pack check of %s: %+v, %v", req.TorrentName, resp, err)
	}
}

func TestMemoryBudget_SeasonPackCheck(t *testing.T) {
	small, large := newBigHeldLibrary(t, biglib.Size/10), newBigHeldLibrary(t, biglib.Size)
	smallSvc := small.checksPacks(small.heldTitles(t.Context(), t, small.releases))
	largeSvc := large.checksPacks(large.heldTitles(t.Context(), t, large.releases))
	budgets := map[string]uint64{"nothing held": biglib.MB / 4, "held show": packCheckBudget}
	for name, req := range packBudgetCases(t) {
		t.Run(name, func(t *testing.T) {
			few := biglib.AllocatedBytes(5, func() { checkSeasonPack(t, smallSvc, req) })
			many := biglib.AllocatedBytes(5, func() { checkSeasonPack(t, largeSvc, req) })

			t.Logf("a check allocates %.3f MB on %d torrents and %.3f MB on %d torrents", float64(few)/biglib.MB, biglib.Size/10, float64(many)/biglib.MB, biglib.Size)
			require.LessOrEqual(t, many, budgets[name])
			require.LessOrEqual(t, many, 2*few)
		})
	}
}

func TestSeasonPackCheckLogBudget(t *testing.T) {
	small, large := newBigHeldLibrary(t, biglib.Size/10), newBigHeldLibrary(t, biglib.Size)
	smallSvc := small.checksPacks(small.heldTitles(t.Context(), t, small.releases))
	largeSvc := large.checksPacks(large.heldTitles(t.Context(), t, large.releases))
	for name, req := range packBudgetCases(t) {
		t.Run(name, func(t *testing.T) {
			few := logbudget.Count(t, func() { checkSeasonPack(t, smallSvc, req) })
			many := logbudget.Count(t, func() { checkSeasonPack(t, largeSvc, req) })

			logbudget.AssertFlat(t, few, many)
			if req.TorrentName == packNotHeld {
				require.Equal(t, logbudget.Lines{{Level: "trace", Message: "season pack: check found no held episode"}: 1}, many)
			}
		})
	}
}

func BenchmarkSeasonPackCheck(b *testing.B) {
	previousLevel := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.WarnLevel)
	b.Cleanup(func() { zerolog.SetGlobalLevel(previousLevel) })
	for _, size := range []int{6_200, 50_000} {
		library := newBigHeldLibrary(b, size)
		source := countedAlternateTitles(3)
		fullScan := library.checksPacks(library.fullScan(library.releases))
		heldTitles := library.checksPacks(library.heldTitles(b.Context(), b, library.releases, source))
		waitForAlternateTitles(b, heldTitles)
		for name, req := range packBudgetCases(b) {
			b.Run(fmt.Sprintf("%d torrents/%s/full scan", size, name), func(b *testing.B) { benchmarkPackCheck(b, fullScan, req, 0) })
			b.Run(fmt.Sprintf("%d torrents/%s/held titles", size, name), func(b *testing.B) { benchmarkPackCheck(b, heldTitles, req, 20*time.Millisecond) })
		}
	}
}

func benchmarkPackCheck(b *testing.B, svc *Service, req *SeasonPackCheckRequest, target time.Duration) {
	b.Helper()
	checkSeasonPack(b, svc, req)
	b.ReportAllocs()
	for b.Loop() {
		checkSeasonPack(b, svc, req)
	}
	if took := b.Elapsed() / time.Duration(b.N); target > 0 && took > target {
		b.Errorf("a check takes %v, want under %v", took, target)
	}
}

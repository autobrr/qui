// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package prefcatalog

var speedEntries = []Entry{
	{Key: "up_limit", Tab: TabSpeed, Section: "globalRateLimits", Control: ControlNumber, WireUnit: "B/s", DisplayUnit: "KiB/s"},
	{Key: "dl_limit", Tab: TabSpeed, Section: "globalRateLimits", Control: ControlNumber, WireUnit: "B/s", DisplayUnit: "KiB/s"},
	{Key: "alt_up_limit", Tab: TabSpeed, Section: "alternativeRateLimits", Control: ControlNumber, WireUnit: "B/s", DisplayUnit: "KiB/s"},
	{Key: "alt_dl_limit", Tab: TabSpeed, Section: "alternativeRateLimits", Control: ControlNumber, WireUnit: "B/s", DisplayUnit: "KiB/s"},
	{Key: "scheduler_enabled", Tab: TabSpeed, Section: "alternativeRateLimits", Control: ControlSwitch},
	{Key: "schedule_from_hour", Tab: TabSpeed, Section: "alternativeRateLimits", Control: ControlNumber, DependsOn: "scheduler_enabled"},
	{Key: "schedule_from_min", Tab: TabSpeed, Section: "alternativeRateLimits", Control: ControlNumber, DependsOn: "scheduler_enabled"},
	{Key: "schedule_to_hour", Tab: TabSpeed, Section: "alternativeRateLimits", Control: ControlNumber, DependsOn: "scheduler_enabled"},
	{Key: "schedule_to_min", Tab: TabSpeed, Section: "alternativeRateLimits", Control: ControlNumber, DependsOn: "scheduler_enabled"},
	{Key: "scheduler_days", Tab: TabSpeed, Section: "alternativeRateLimits", Control: ControlSelect, OptionSource: SourceFixed, Options: fixedOptions(0, 1, 2, 3, 4, 5, 6, 7, 8, 9), DependsOn: "scheduler_enabled"},
	{Key: "limit_utp_rate", Tab: TabSpeed, Section: "rateLimitsSettings", Control: ControlSwitch},
	{Key: "limit_tcp_overhead", Tab: TabSpeed, Section: "rateLimitsSettings", Control: ControlSwitch},
	{Key: "limit_lan_peers", Tab: TabSpeed, Section: "rateLimitsSettings", Control: ControlSwitch},
}

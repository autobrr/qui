// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dbinterface

import (
	"fmt"
	"time"
)

// sqliteTimeLayout is the form in which the database module makes the SQLite
// driver write a bound time (ADR 0045).
const sqliteTimeLayout = "2006-01-02 15:04:05.999999999-07:00"

// NullTime scans a nullable time from a computed column, such as MIN or MAX of
// a time column. Postgres returns such a column as a time. SQLite returns the
// stored text, which sql.NullTime rejects. Time is in UTC when the source is
// text. Scan returns an error for text in any other form than the two that the
// database module stores.
type NullTime struct {
	Time  time.Time
	Valid bool
}

func (n *NullTime) Scan(src any) error {
	n.Time, n.Valid = time.Time{}, false

	switch v := src.(type) {
	case nil:
		return nil
	case time.Time:
		n.Time, n.Valid = v, true
		return nil
	case string:
		parsed, err := time.Parse(sqliteTimeLayout, v)
		if err != nil {
			parsed, err = time.ParseInLocation(time.DateTime, v, time.UTC)
		}
		if err != nil {
			return fmt.Errorf("scan time: %q is not in a form that the database module stores", v)
		}
		n.Time, n.Valid = parsed.UTC(), true
		return nil
	default:
		return fmt.Errorf("scan time: unsupported source type %T", src)
	}
}

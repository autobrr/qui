// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package fsutil

import "os"

// ContentDirMode is the mode passed to os.MkdirAll for directories that hold
// torrent content / hardlink / reflink trees. It is intentionally 0o777 so the
// process umask controls the final permissions (0o777 & ~022 = 0755 default;
// 0o777 & ~002 = 0775 for shared-group setups where qBittorrent and qui run as
// different users). See discussion #1704.
//
// Do NOT tighten this to 0755. That re-breaks umask-controlled group write: the
// group-write bit can only be preserved by the umask if it is requested here
// first.
const ContentDirMode os.FileMode = 0o777

// LinkTreeBaseDirMode is like ContentDirMode but for base directories that were
// historically created without any "other" access (the dir-scan hardlink base
// was 0o750). It carries no other-bits, so it can never grant world access
// regardless of umask, while still letting the umask decide group permissions
// (0o770 & ~022 = 0750 default; 0o770 & ~002 = 0770 for shared-group setups).
const LinkTreeBaseDirMode os.FileMode = 0o770

//go:build windows

package orphanscan

import "io/fs"

func inodeKeyFromInfo(info fs.FileInfo) (inodeKey, uint64, bool) {
	return inodeKey{}, 0, false
}

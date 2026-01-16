//go:build !windows

package orphanscan

import (
	"errors"
	"syscall"
)

func isReadOnlyFSError(err error) bool {
	return errors.Is(err, syscall.EROFS)
}

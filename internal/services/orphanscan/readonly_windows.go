//go:build windows

package orphanscan

func isReadOnlyFSError(err error) bool {
	return false
}

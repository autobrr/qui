package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var Dist embed.FS

// DistDirFS creates a sub-filesystem rooted at the dist directory
var DistDirFS = mustSubFS(Dist, "dist")

// mustSubFS creates sub FS from current filesystem or panic on failure.
// This is similar to autobrr's implementation.
func mustSubFS(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		// If dist doesn't exist (e.g., in development), return a dummy FS
		return &dummyFS{}
	}
	return sub
}

// dummyFS is a dummy filesystem that returns "not found" for all files
type dummyFS struct{}

func (d *dummyFS) Open(name string) (fs.File, error) {
	return nil, fs.ErrNotExist
}

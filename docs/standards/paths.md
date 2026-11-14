# Path standards

qui must work on Windows and Unix-like hosts.

- Host-only paths (data dir, backups): `filepath.Join`, `filepath.Clean`, `filepath.Rel`, `filepath.Separator`.
- Paths to or from an `fsops.Backend`, including save paths from qBittorrent: use `backend.Paths()`. A remote backend uses slash paths, and host `filepath` changes them on a Windows host.
- Slash-delimited formats: use `path` for torrent-internal file names, the path part of a URL, and API fields that hold a slash path. Build a full URL with `net/url`. An API field that holds a filesystem path keeps the dialect of that filesystem.
- At torrent/API -> local FS boundaries: validate slash paths, then convert with `filepath.FromSlash`.
- A path that qui sends to the filesystem or to qBittorrent keeps its UNC root (`\\server\share`), its drive root, and its letter case. A lowercased path is a comparison key only.
- Traversal checks must reject POSIX + Windows escaping on every OS: leading `/`, leading `\`, drive letters, UNC, `..`.
- Cross-platform tests: avoid raw `"/foo/"` local path assertions; use `filepath.ToSlash` or `filepath.Join`.
- Path traversal tests should include POSIX and Windows cases.

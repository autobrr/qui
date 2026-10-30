# Reflink detection in hardlink scope

qui does not detect reflinks (block clones) when it sets `HARDLINK_SCOPE`. Scope comes only from the inode and the `nlink` count. A reflink has its own inode, and cloning does not raise `nlink`.

## Why

- ZFS has no per-file query for shared blocks. It does not implement the FIEMAP ioctl ([openzfs/zfs#264](https://github.com/openzfs/zfs/issues/264) is open, and [#7545](https://github.com/openzfs/zfs/pull/7545) closed without merge). The open [#9554](https://github.com/openzfs/zfs/pull/9554) marks only dedup blocks as shared, not block clones. ZFS reports block cloning only as pool totals (`bcloneused`, `bclonesaved`). The user who reported this uses ZFS block cloning.
- On btrfs and XFS, FIEMAP tells that an extent is shared, but not with which file ([fiemap.rst](https://docs.kernel.org/filesystems/fiemap.html)). Hardlink scope must tell library links (`outside_qbittorrent`) apart from links between torrents (`torrents_only`). btrfs (`BTRFS_IOC_LOGICAL_INO`) and XFS (`FS_IOC_GETFSMAP`) can name the files that share an extent, but only as root. qui normally runs without root.
- On btrfs, an extent that a snapshot also holds is marked as shared. A torrent on a snapshotted subvolume then looks linked when nothing else uses it.
- FIEMAP works only on Linux, and only on filesystems that implement it. NFS and FUSE do not. qui also runs on Windows, macOS, and over SFTP.
- Content hashing works on every filesystem, but it reads every byte of every torrent and library file. For large libraries, that cost is too high for a scan that runs with every automation.

With hardlinks, scope detection works on every filesystem that reports accurate `nlink` values. The automations documentation tells users to link the library with hardlinks when their rules depend on `HARDLINK_SCOPE`. `fclones link` makes hardlinks, and `fclones dedupe` makes reflinks ([fclones README](https://github.com/pkolaczk/fclones#usage)).

Reconsider when ZFS reports block-cloned extents through FIEMAP or another per-file API, and when qui can name the files that share an extent without root.

## Prior requests

- #1539

---
status: accepted
date: 2026-10-28
---

# A shared file on another instance is found on disk, and an automation keeps the files

Two qBittorrent instances can save into one folder on one disk. The batch matcher then finds a Shared file between a torrent on one instance and a torrent on another, when qui reads the files of both instances on its own disk. Two paths are one folder entry when their parent folders have the same file identity and the file names are equal. The batch matcher checks only likely pairs: the same path text, name, release, or total size (#3585).

The Cross-Seed tab labels such a torrent Content. The Delete dialog lists it and can delete it. This changes ADR 0032, where the Delete dialog keeps only the torrents on its instance. An automation never reaches another instance. In the modes that preserve or include cross-seeds, it deletes the torrent on its own instance without the files. While another instance with local file access is offline, these modes keep the files of each torrent that they delete. An instance that is turned off in qui is named in the Delete dialog and ignored by automations.

## Considered options

- **Compare path text across instances.** Rejected: two mounts of one folder have different path text, and the same path text on two instances is not proof of one folder.
- **Check every file of every torrent on the other instances.** Rejected: a select-all delete of a large library reads tens of thousands of files.
- **An automation that also deletes the torrent on the other instance.** Rejected: a rule then acts outside its own instance.
- **An automation that treats a turned-off instance as offline.** Rejected: a turned-off instance can stay off for good, and the modes that preserve or include cross-seeds then never delete files.

## Consequences

- A torrent on another instance that reads a file inside the folder of the source through a second mount, with a different name, release, and total size, is not found.
- qui does not check SSH instances, because qui cannot make sure that the file identity that a remote host reports is correct.
- A free space automation in the modes that preserve or include cross-seeds frees nothing while another local instance is offline.

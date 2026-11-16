---
status: accepted
date: 2026-11-16
---

# qui copies file bytes with its own SFTP client and resumes by proven pieces

A Transfer copies file bytes with the SFTP client that qui already uses. qui checks each piece against the .torrent while the bytes pass through it. A piece is a Proven piece when it passed this check and the target confirmed the flush of its bytes. The Transfer record keeps the Proven pieces. After a restart or a dropped connection, qui copies only the pieces that are not proven. qui calls no rsync, rclone, or tar, and installs nothing on the remote host. A `depguard` rule keeps the SFTP library inside the SSH pool and the file layer.

Proof and resume decide this choice, and speed comes second. A copy tool compares the copy with the source file. The .torrent has a hash for each piece, so qui proves each part of the copy without a second read of the source. One record, the Proven pieces, gives the resume and the proof that qui needs before it removes the source.

A target that cannot confirm a flush has no Proven pieces. This is an SFTP server without the `fsync@openssh.com` extension. OpenSSH added the extension in version 6.5. After an interruption there, a check-only helper torrent checks the partial files on the target, and qui copies the pieces that failed.

For speed, the copy uses four SSH connections for each Transfer. A big file splits into four byte ranges, and small files go many at a time. The measurements came from a test server at Hetzner, 28 ms away over a line of about 1 Gbit (raw TCP: 90 MiB/s on one stream, 103 MiB/s on four). Pull of a 2 GiB episode, a 15 GiB pack of ten files, a 10 GiB movie, and a 50 GiB remux: SFTP on four connections 92, 102, 100, and 100 MiB/s. rsync, a tar stream, and SFTP on one connection 62 to 65 MiB/s. rclone with tuned flags 66 to 68 MiB/s, with 111 to 248 MiB of RAM on the qui host against 22 to 53 MiB for SFTP. Eight connections added nothing on this line. The x/crypto SSH library gives each channel a fixed window of 2 MiB, so one connection moves at most about 2 MiB for each round trip.

Small files go up to 128 at a time across the connections. In tests, 2,000 files of 128 KiB moved at 30 MiB/s this way, against 6 MiB/s with 8 at a time. A piece that crosses the edge of a byte range or of a file is checked when all of its bytes arrived. A host that refuses extra connections gets the copy on fewer, down to one, and the Transfer goes on. There is no setting for the count.

## Considered options

- **rsync over ssh.** Rejected. Resume: with `--partial`, rsync keeps a file that stopped halfway and continues it. It compares the copy with the source file and not with the .torrent, so qui must still check every piece after it. Tools: the Docker image and Windows have no rsync or ssh client, and an SFTP-only seedbox key cannot run it. Speed: one SSH connection, so it stops near 63 MiB/s like plain SFTP. Its delta transfer gives nothing for files that do not change.
- **rclone, as a binary or a library.** Rejected. Resume: rclone has no option to continue a file that stopped halfway. It writes the file under a `.partial` name and starts it again. Speed: not faster than SFTP on four connections on the real line, and several times the RAM on the qui host and on the seedbox.
- **tar streams over an exec session.** Rejected. Resume: a stream that stops cannot continue inside a file. It also needs a shell on the remote host. Speed: one stream is as fast as one SFTP connection for media files. It only wins on thousands of tiny files, where many SFTP files in flight close most of the gap.
- **Direct host-to-host with agent forwarding.** Rejected: it lends qui's key to the source host and needs a shell there.
- **The WebAPI file download of qBittorrent 5.3.** Rejected for now: some downloads of a 1 GiB file ended short in tests.

## Consequences

- qui needs no extra tool on any host, also on Windows.
- The resume is the same on one connection and on four, because the record holds pieces and not byte offsets.
- Four connections fit the measured line. A line with a much longer round trip can need more. Revisit this when users report slow Transfers on long routes.
- The BitTorrent way between the two clients stays as the way without SSH. In that way, qBittorrent resumes the download itself.

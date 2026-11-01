---
status: accepted
date: 2026-11-01
---

# A signing key change ships one bridge release

Self-update trusts one certificate, `internal/update/release_signing_cert.pem`, which is built into the binary. It takes the release with the highest version that is not a draft or a prerelease, and it accepts any tag that contains three numbers, not only a `v` tag. It downloads `checksums.txt` and accepts the release only if `checksums.txt.sig` matches the certificate. An installed qui keeps the certificate of its own version until it updates, and no change on our side can take that trust away.

To change the signing key, we ship one bridge release. The bridge release embeds the new certificate and is signed with the old key, so each install accepts it and gets the new certificate. Each release after it is signed with the new key. During the change, the release workflow signs with the key that matches the certificate of the previous release, and holds both keys in the `release` environment (#3695). After the bridge release, the workflow goes back to one key and checks it against the certificate in the tagged commit (#3699). The next key change can copy the two-key step from #3695.

The old key stays a secret after the change, because installs that did not update still trust it. Two repository settings limit what a person with write access can do with it:

- Immutable releases are on. GitHub locks the files and the tag of a release when it is published. The release stays a draft until the Release workflow publishes it, so a failed run can still be retagged before that.
- The ruleset "Release tags: admins only" covers all tags. Only an admin can create, move, or delete a tag, so a person with write access cannot add a release with a higher version. The release workflow builds only `v*` tags, but the rule covers all tags, because Self-update also takes a release that a person makes by hand on a tag without a `v`.

`release.preflight.fail_on_error` in the GoReleaser files stops a re-run of a published tag before the build, because the upload to an immutable release fails at the end anyway.

## Consequences

- A user who skips the bridge release gets a signature error from Self-update and must download the next version by hand one time.
- Docker images do not self-update, so a key change does not affect them.
- A published release cannot be fixed. If its files are wrong, we ship the next patch version.
- Immutable releases do not lock the releases that were published before the setting, and a person with write access can still delete a release. Delete the old key after the bridge release, and treat a leak of it as an incident.

## Considered options

- **Sign each release twice.** Each release ships `checksums.txt.sig` signed with the old key, and a second checksums file with a signature from the new key, which new builds check. Users who skip the bridge release keep updating. Rejected: it adds a second checksums file to each release and keeps the old key in use for an open-ended time, to spare a one-time manual download.

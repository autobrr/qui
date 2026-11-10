---
sidebar_position: 3.5
title: Reach the files of a qBittorrent instance over SSH
sidebar_label: SSH Access
description: Set up SSH access so that orphan scan, Dir Scan, hardlink mode, and other file features work on a qBittorrent instance whose files are on another machine.
---

# SSH Access

qui always talks to qBittorrent over the Web API. Some features also read or change files on disk, for example orphan scan and cross-seed hardlink mode. SSH access gives qui those files when qBittorrent runs on another machine, such as a seedbox. You install nothing on that machine. qui uses the SSH server that is already there.

A **remote instance** is an instance whose files qui reaches over SSH. The other pages of this documentation use that term.

If qui runs on the same machine as qBittorrent, or sees the same files through a mount, use [Local Filesystem Access](./instance-settings.md#local-filesystem-access) instead. It supports every feature.

## Requirements

- An SSH login on the machine that holds the files of the instance.
- Key authentication for that login. qui does not log in with a password.
- An SSH server with SFTP. Most servers and seedboxes have it.

Some features need more from the server. The connection test shows what your server allows. See [What works over SSH](#what-works-over-ssh).

## Set up SSH access

### 1. Create a key

qui needs an OpenSSH private key without a passphrase. Create a key that only qui uses. On your own computer, run:

```bash
ssh-keygen -t ed25519 -N "" -C qui -f qui_seedbox
```

The command writes two files. `qui_seedbox` is the private key, and `qui_seedbox.pub` is the public key.

### 2. Add the public key on the server

Add the line in `qui_seedbox.pub` to `~/.ssh/authorized_keys` of your user on the server. If you can log in with a password, this command does that:

```bash
ssh-copy-id -i qui_seedbox.pub user@seedbox.example.com
```

- If the SSH server does not use port 22, add `-p <port>`.
- If the provider limits the login to SFTP, add `-s`. The command then adds the key over SFTP.
- If the command asks you to accept a host key, compare the fingerprint with the one that the server has before you type `yes`. [Step 4](#4-test-the-connection-and-confirm-the-host-key) tells you where to find it.

Some seedbox providers have a page for SSH keys in their control panel. Paste the content of `qui_seedbox.pub` there.

### 3. Enter the settings in qui

1. [Edit the instance](./instance-settings.md#edit-an-instance) and open the **SSH** tab. The Add Instance form has no SSH fields, so add the instance first.
2. Make sure that **Local Filesystem Access** on the **Instance** tab is off. See [Local Filesystem Access comes first](#local-filesystem-access-comes-first).
3. Fill in the fields:

   | Field | Description |
   |-------|-------------|
   | **SSH host** | The hostname or IP address of the machine that holds the files, without a scheme or a port. |
   | **Port** | The port of the SSH server. The default is 22. |
   | **Username** | The SSH user. |
   | **Private key** | The full content of the private key file, `qui_seedbox` in the example. |

4. Click **Save SSH settings**.

qui stores the private key encrypted and does not show it again. You must paste the key again if you change the host, the port, or the username, or if [the encryption key of qui changes](#the-encryption-key-of-qui-changed). Keep the private key file in a safe place, for example a password manager. If you delete the file, create a new key and add its public key on the server when you need one.

### 4. Test the connection and confirm the host key

1. Click **Test connection**. A test uses the saved settings, so save your changes first.
2. The first test shows **First contact** and the fingerprint of the host key. The host key identifies the server. Compare the fingerprint with the one that the server has. On the server, this command prints it:

   ```bash
   ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub
   ```

   Some providers show the fingerprint in their control panel. If the server uses another key type, use the matching `ssh_host_*_key.pub` file.
3. If the fingerprints are the same, click **Confirm host key**. qui pins this key. From then on, qui refuses to connect if the server presents another key.
4. Click **Test connection** again. The result shows what the server allows.

qui uses SSH for the instance only after you confirm the host key. Until then, the instance has no filesystem access.

## Read the test result

The test shows a tier and a list of checks.

| Line | Meaning |
|------|---------|
| **Tier: SFTP + commands** | qui can run commands on the host. qui can read file identity there, which tells qui when two paths are the same file. |
| **Tier: SFTP** | qui only has SFTP. qui cannot read file identity. |
| **SFTP** | The server has SFTP. qui needs it for all file access. |
| **Free space (statvfs)** | The server reports free disk space. The automation setting **Path on server** needs it. |
| **Hardlinks (hardlink@openssh.com)** | The server can create hardlinks. Cross-seed hardlink mode needs it. |
| **Limits extension** | The server reports its SFTP limits. No feature needs it. |
| **Commands** | The login can run a shell. |
| **GNU find and stat** | The host has the GNU versions of `find` and `stat`. |

qui gets the tier **SFTP + commands** when the login runs a POSIX shell and the host has GNU `find` and `stat`. A normal Linux server has both. qui gets the tier **SFTP** in these cases:

- The provider limits the login to SFTP.
- The login shell is not a POSIX shell, for example fish.
- The host has other versions of the tools, for example BSD or macOS.

The other pages name the two tiers by what qui can do:

- "a remote instance where qui can run commands on the host" is the tier **SFTP + commands**.
- "a remote instance where qui only has SFTP" is the tier **SFTP**.

qui tests again each time that it connects, and the tier follows the result. In **Settings → Instances**, the card of a remote instance shows **Over SSH**. Point at the label to read the limits of the instance. The SSH tab shows the same limits below the settings.

## What works over SSH

| Feature | Local Filesystem Access | SSH, qui can run commands | SSH, SFTP only |
|---------|:-----------------------:|:-------------------------:|:--------------:|
| [Orphan scan](./orphan-scan.md): scan, preview, and export | Yes | Yes | Yes |
| Orphan scan: deletion and auto-cleanup | Yes | Yes | No |
| [Directory Scanner](./cross-seed/dir-scan.md) | Yes | Yes | Yes, but it cannot detect files that already seed |
| Cross-seed [hardlink mode](./cross-seed/hardlink-mode.md), [season pack](./cross-seed/season-packs.md) assembly, and manual assembly | Yes | Yes | Yes, but any existing link target stops the add |
| Cross-seed reflink mode | Yes | No | No |
| Cross-seed [pooled partial completion](./cross-seed/hardlink-mode.md#pooled-partial-completion) | Yes | No | No |
| Cross-seed route to a [Cross-seed instance](./cross-seed/hardlink-mode.md#cross-seed-instance) | Yes | No | No |
| Removal of [leftover folders](./torrent-management.md#deleting-and-moving-torrents) after a delete or a move | Yes | Yes | Yes |
| Automations: [Path on server](./automations.md#free-space-source) for free space | Yes | Yes | Yes |
| Automations: Has Missing Files condition | Yes | Yes | Yes |
| Automations: Hardlink Scope conditions, hardlink grouping, and **Include hardlinked copies** | Yes | No | No |
| Detection of hardlinked cross-seeds in the delete dialog | Yes | No | No |
| Content file download, MediaInfo, and BDInfo | Yes | No | No |
| Cross-seed ID fallback from MKV tags | Yes | No | No |

Hardlink mode needs the check **Hardlinks (hardlink@openssh.com)**, and **Path on server** needs **Free space (statvfs)**, on both tiers.

qui does not use the file identity that a remote host reports to add torrents or files to a delete. That is why the hardlink conditions and the hardlink detection in the delete dialog work only with Local Filesystem Access.

## Local Filesystem Access comes first

If **Local Filesystem Access** is on, qui reads the files of the instance on the machine that runs qui, and does not use the SSH settings. The SSH tab shows a note when this is the case.

For a remote instance, keep Local Filesystem Access off. If you turn it on, qui looks for the paths of the remote machine on its own disk. The file features then fail or return wrong results.

## The host key changed

If the server presents a key that is not the pinned key, the test shows **Host key changed**, and qui refuses to connect. The file features of the instance stop until you act.

- If you expect the change, for example because the provider reinstalled the server, compare the new fingerprint with the server. Then click **Replace host key**.
- If you do not expect the change, do not replace the key. Ask your provider first. Another machine can answer at that address.

A change of the SSH host or port in qui also drops the pinned key. The next test is a first contact again.

If the test shows **Pinned key unreadable**, qui cannot decrypt the pinned host key. Compare the fingerprint in the test result with the one that the server has, as for a first contact. If the fingerprints are the same, click **Replace host key**.

If the result shows no fingerprint, the server did not answer. Make sure that the server is on and that the SSH host and port are correct. Then test again.

### The encryption key of qui changed

qui makes its encryption key from [`sessionSecret`](../configuration/reference.md). If that value changes, qui cannot read the stored private key or the pinned host key. The test then shows **Connection failed** with the text "Failed to read SSH credentials", and no host key.

1. Paste the private key again and click **Save SSH settings**.
2. Click **Test connection**. The test shows **Pinned key unreadable** and the fingerprint of the host key.
3. Compare the fingerprint with the one that the server has. If the fingerprints are the same, click **Replace host key**.

## Change or remove SSH access

- To change the key, paste a new private key and click **Save SSH settings**.
- To remove SSH access, click **Remove SSH access**. qui deletes the stored username and private key. The instance then has no filesystem access. qui keeps the SSH host, the port, and the pinned host key, because the host key belongs to the server. If you later save a username and a key for the same host and port, the instance is a remote instance again, and qui does not ask you to confirm the host key.

While cross-seed hardlink or reflink mode is on for the instance, qui refuses two changes: a change of the SSH host or port, and **Remove SSH access**. Turn off the link mode of the instance first.

After you save, remove, or replace the SSH settings, run a new [orphan scan](./orphan-scan.md#remote-instances) before you delete. qui does not delete from a scan that it made with the earlier settings.

## Paths on a remote instance

Each path that you enter for a remote instance is a path on the remote machine: hardlink base directories, Dir Scan directories, orphan scan ignore paths, and the **Path on server** for free space. Enter the path as the SSH user sees it.

If qBittorrent runs in a container on the remote machine, its save paths must be the same inside the container and for the SSH user. qui reads the save paths from qBittorrent and opens them over SSH.

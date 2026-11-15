#!/usr/bin/env bash
# launch.sh <qui binary> <torrent count> <scenario> <out dir> [webapi version]
# Caps the whole rig at 5 GiB with no swap, so a runaway is killed and cannot
# freeze the machine, and runs it in a loopback-only network namespace.
RIG=$(dirname "$(readlink -f "$0")")
exec systemd-run --user --scope --quiet -p MemoryMax=5G -p MemorySwapMax=0 \
  unshare -rn bash "$RIG/run.sh" "$@"

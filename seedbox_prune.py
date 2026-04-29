#!/usr/bin/env python3
"""
seedbox_pruner: Smart disk space management for seedbox

Uses the Qui API to delete torrents, which automatically finds and removes
cross-seeded copies across all configured instances.

Deletes torrents from the 'delete' category based on:
- Only inactive torrents (no upload/download activity)
- Sorted by popularity (least popular first)
- Iterative deletion (delete 1 → check disk → repeat until under threshold)
"""

import os
import sys
import time
import argparse
import re
import warnings
from pathlib import Path
from typing import Optional, List

import requests

# Optional paramiko; we will gracefully fall back to system ssh if unavailable or fails
try:
    import paramiko
except Exception:
    paramiko = None


# -----------------------------
# Logging
# -----------------------------
def log(msg: str) -> None:
    ts = time.strftime("%Y-%m-%d %H:%M:%S")
    print(f"[{ts}] {msg}", flush=True)


# -----------------------------
# .env loader (simple, no deps)
# -----------------------------
def load_env_file(env_path: str) -> None:
    p = Path(env_path)
    if not p.exists():
        log(f"[env] ENV_FILE not found: {env_path}")
        return

    for raw in p.read_text(errors="replace").splitlines():
        line = raw.strip()
        if not line or line.startswith("#"):
            continue
        if line.startswith("export "):
            line = line[len("export "):].strip()
        if "=" not in line:
            continue
        k, v = line.split("=", 1)
        k = k.strip()
        v = v.strip().strip("'").strip('"')
        os.environ.setdefault(k, v)


def need_env(*keys: str) -> None:
    for k in keys:
        if not os.getenv(k):
            raise SystemExit(f"Missing required env var: {k}")


def env_bool(key: str, default: bool = False) -> bool:
    v = os.getenv(key)
    if v is None:
        return default
    return v.strip().lower() in ("1", "true", "yes", "y", "on")


def env_int(key: str, default: int) -> int:
    v = os.getenv(key)
    if v is None:
        return default
    try:
        return int(v.strip())
    except Exception:
        return default


# -----------------------------
# Qui API helper
# -----------------------------
class QuiClient:
    def __init__(self, base_url: str, api_key: str, verify_ssl: bool = True):
        self.base = base_url.rstrip("/")
        self.sess = requests.Session()
        self.sess.verify = verify_ssl
        self.sess.headers["X-API-Key"] = api_key
        self.sess.headers["Content-Type"] = "application/json"

    def get_instances(self) -> List[dict]:
        r = self.sess.get(f"{self.base}/api/instances", timeout=30)
        r.raise_for_status()
        return r.json()

    def get_torrents(self, instance_id: int, category: str, page: int = 0, limit: int = 2000) -> dict:
        filters = {
            "status": [],
            "excludeStatus": [],
            "categories": [category],
            "excludeCategories": [],
            "tags": [],
            "excludeTags": [],
            "trackers": [],
            "excludeTrackers": [],
        }
        import json
        params = {
            "page": str(page),
            "limit": str(limit),
            "filters": json.dumps(filters),
        }
        r = self.sess.get(
            f"{self.base}/api/instances/{instance_id}/torrents",
            params=params,
            timeout=60,
        )
        r.raise_for_status()
        return r.json()

    def get_cross_seed_matches(self, instance_id: int, torrent_hash: str) -> List[dict]:
        r = self.sess.get(
            f"{self.base}/api/cross-seed/torrents/{instance_id}/{torrent_hash}/local-matches",
            params={"strict": "true"},
            timeout=60,
        )
        r.raise_for_status()
        data = r.json()
        return data.get("matches") or []

    def delete_torrents(
        self,
        instance_id: int,
        hashes: List[str],
        targets: Optional[List[dict]] = None,
        delete_files: bool = True,
    ) -> None:
        payload = {
            "action": "delete",
            "hashes": hashes,
            "deleteFiles": delete_files,
        }
        if targets:
            payload["targets"] = targets
        r = self.sess.post(
            f"{self.base}/api/instances/{instance_id}/torrents/bulk-action",
            json=payload,
            timeout=60,
        )
        r.raise_for_status()


# -----------------------------
# HDD usage over SSH
# -----------------------------
def ssh_run(host: str, port: int, user: str, password: Optional[str], key_path: Optional[str], cmd: str) -> str:
    """Run a command over SSH and return stdout (stripped)."""
    key_ok = bool(key_path and Path(key_path).exists())

    # Try paramiko if available
    if paramiko is not None:
        client = paramiko.SSHClient()
        client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
        try:
            if key_ok:
                client.connect(
                    hostname=host,
                    port=port,
                    username=user,
                    key_filename=key_path,
                    look_for_keys=False,
                    allow_agent=False,
                    timeout=30,
                )
            else:
                client.connect(
                    hostname=host,
                    port=port,
                    username=user,
                    password=password,
                    look_for_keys=False,
                    allow_agent=False,
                    timeout=30,
                )
            stdin, stdout, stderr = client.exec_command(cmd, timeout=60)
            out = stdout.read().decode(errors="replace").strip()
            err = stderr.read().decode(errors="replace").strip()
            if err:
                log(f"[ssh][warn] {err}")
            return out
        except Exception as e:
            log(f"[ssh][warn] paramiko failed; falling back to system ssh: {e}")
        finally:
            try:
                client.close()
            except Exception:
                pass

    # Fallback: system ssh
    import subprocess
    ssh_cmd = ["ssh", "-o", "StrictHostKeyChecking=no", "-p", str(port)]
    if key_ok:
        ssh_cmd += ["-i", key_path]
    ssh_cmd += [f"{user}@{host}", cmd]

    p = subprocess.run(ssh_cmd, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, timeout=90)
    if p.stderr.strip():
        log(f"[ssh][warn] {p.stderr.strip()}")
    return p.stdout.strip()


def parse_usage_gib(text: str) -> Optional[int]:
    """
    Parse disk usage output like '916 GiB' or '0.89 TiB'
    Also handles ANSI color codes like '\x1b[33mused 926GiB\x1b[0m'
    """
    if not text:
        return None

    # Strip ANSI color codes
    ansi_escape = re.compile(r'\x1b\[[0-9;]*m')
    clean_text = ansi_escape.sub('', text)

    # Look for number + unit pattern
    m = re.search(r"([0-9]+(?:\.[0-9]+)?)\s*([A-Za-z]+)?", clean_text.strip())
    if not m:
        return None

    val = float(m.group(1))
    unit = (m.group(2) or "").strip().lower()

    if unit in ("tib", "ti", "t"):
        return int(round(val * 1024))
    if unit in ("gib", "gi", "g", ""):
        return int(round(val))
    if unit == "tb":
        return int(round(val * 1000 * 1000 * 1000 * 1000 / (1024**3)))
    if unit == "gb":
        return int(round(val * 1000 * 1000 * 1000 / (1024**3)))

    return None


def get_seedbox_usage_gib() -> Optional[int]:
    host = os.getenv("SEEDBOX_HOST")
    user = os.getenv("SEEDBOX_SSH_USER")
    cmd = os.getenv("SEEDBOX_HDDUSAGE_CMD")
    if not (host and user and cmd):
        return None

    port = int(os.getenv("SEEDBOX_SSH_PORT", "22"))
    pw = os.getenv("SEEDBOX_SSH_PASS")
    key = os.getenv("SEEDBOX_SSH_KEY")

    try:
        out = ssh_run(host, port, user, pw, key, cmd)
    except Exception as e:
        log(f"[disk][warn] SSH usage command failed: {e}")
        return None

    gib = parse_usage_gib(out)
    if gib is None:
        log(f"[disk][warn] Could not parse HDD usage output as GiB: {out!r}")
    return gib


# -----------------------------
# Main pruning logic
# -----------------------------
def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--live", action="store_true", help="Actually perform actions (not dry-run)")
    args = ap.parse_args()

    dry_run = not args.live
    log(f"Starting seedbox_pruner (dry_run={dry_run})")

    # Load env
    env_file = os.getenv("ENV_FILE", "/mnt/pool/stephen/qbt-stack.env")
    log(f"[config] ENV_FILE={env_file}")
    load_env_file(env_file)

    # Required envs — now Qui instead of qBittorrent direct
    need_env("QUI_URL", "QUI_API_KEY")

    # Categories
    delete_cat = os.getenv("DELETE_CAT", "delete")

    # Behavior knobs
    prune_threshold_gib = env_int("SEEDBOX_PRUNE_THRESHOLD_GIB", 800)
    prune_max = env_int("SEEDBOX_PRUNE_MAX", 50)  # Safety limit per run
    check_interval_sec = env_int("PRUNE_CHECK_INTERVAL_SEC", 3)  # Wait time between deletions

    # Instance selection — name or ID of the seedbox instance in Qui
    seedbox_instance = os.getenv("QUI_SEEDBOX_INSTANCE", "")

    log(f"[config] DELETE_CAT={delete_cat}")
    log(f"[config] PRUNE_THRESHOLD={prune_threshold_gib} GiB")
    log(f"[config] MAX_DELETIONS_PER_RUN={prune_max}")
    log(f"[config] CHECK_INTERVAL={check_interval_sec}s")
    log(f"[config] QUI_SEEDBOX_INSTANCE={seedbox_instance or '(auto-detect)'}")

    # SSL verification
    verify_ssl = env_bool("QUI_VERIFY_SSL", True)
    if not verify_ssl:
        try:
            from urllib3.exceptions import InsecureRequestWarning
            warnings.filterwarnings("ignore", category=InsecureRequestWarning)
            import urllib3
            urllib3.disable_warnings(InsecureRequestWarning)
        except Exception:
            pass

    qui = QuiClient(os.getenv("QUI_URL"), os.getenv("QUI_API_KEY"), verify_ssl=verify_ssl)

    # Resolve instance ID
    instances = qui.get_instances()
    if not instances:
        log("[qui] ERROR: No instances configured in Qui")
        sys.exit(1)

    instance_id = None
    if seedbox_instance:
        # Match by name (case-insensitive) or numeric ID
        for inst in instances:
            if str(inst.get("id")) == seedbox_instance:
                instance_id = inst["id"]
                break
            if inst.get("name", "").lower() == seedbox_instance.lower():
                instance_id = inst["id"]
                break
        if instance_id is None:
            available = ", ".join(f"{i['id']}={i.get('name', '?')}" for i in instances)
            log(f"[qui] ERROR: Instance '{seedbox_instance}' not found. Available: {available}")
            sys.exit(1)
    else:
        # Auto-select the first (or only) instance
        instance_id = instances[0]["id"]

    instance_name = next((i.get("name", "?") for i in instances if i["id"] == instance_id), "?")
    log(f"[qui] Using instance: {instance_name} (id={instance_id})")

    # --- Check disk usage and prune if needed ---
    initial_usage = get_seedbox_usage_gib()
    if initial_usage is None:
        log("[pruner] ERROR: Could not get seedbox disk usage")
        sys.exit(1)

    log(f"[pruner] Seedbox usage: {initial_usage} GiB")

    if initial_usage <= prune_threshold_gib:
        log(f"[pruner] Usage {initial_usage} GiB is below threshold {prune_threshold_gib} GiB - no pruning needed")
        log("\nseedbox_pruner done.")
        return

    log(f"[pruner] Usage > {prune_threshold_gib} GiB - starting iterative pruning")

    deleted_count = 0

    if dry_run:
        # Dry-run: fetch once, show what would be deleted in order
        resp = qui.get_torrents(instance_id, category=delete_cat)
        dels = resp.get("torrents") or []

        if not dels:
            log("[pruner] No torrents in delete category")
        else:
            inactive = [t for t in dels if t.get("dlspeed", 0) == 0 and t.get("upspeed", 0) == 0]
            if not inactive:
                log(f"[pruner] No inactive torrents (all {len(dels)} have activity)")
            else:
                inactive_sorted = sorted(inactive, key=lambda x: x.get("popularity", 0))
                log(f"[pruner] Found {len(inactive_sorted)} inactive torrents (out of {len(dels)} total)")

                for i, to_delete in enumerate(inactive_sorted[:prune_max]):
                    torrent_name = to_delete.get("name", "")
                    torrent_hash = to_delete.get("hash")
                    torrent_size_gib = to_delete.get("size", 0) / (1024**3)
                    popularity = to_delete.get("popularity", 0)

                    log(f"\n[pruner] [{i + 1}/{min(len(inactive_sorted), prune_max)}] {torrent_name[:70]}")
                    log(f"[pruner]   Size: {torrent_size_gib:.2f} GiB  Popularity: {popularity:.3f}")

                    try:
                        all_matches = qui.get_cross_seed_matches(instance_id, torrent_hash)
                        cross_seeds = [cs for cs in all_matches if cs.get("instance_id") == instance_id]
                        skipped = len(all_matches) - len(cross_seeds)
                        if cross_seeds:
                            log(f"[pruner]   Cross-seeds (same instance): {len(cross_seeds)}")
                            for cs in cross_seeds:
                                log(f"[pruner]     - {cs.get('name', '?')[:60]}")
                        if skipped:
                            log(f"[pruner]   Skipping {skipped} cross-seed(s) on other instances")
                        total = 1 + len(cross_seeds)
                        log(f"[pruner]   [DRY] Would delete {total} torrent(s)")
                    except Exception as e:
                        log(f"[pruner]   Cross-seed lookup failed: {e}")
                        log(f"[pruner]   [DRY] Would delete 1 torrent (primary only)")

                    deleted_count += 1

    else:
        # Live mode: iterative delete-one, check-disk loop
        for iteration in range(prune_max):
            # Check current disk usage
            current_usage = get_seedbox_usage_gib()
            if current_usage is None:
                log("[pruner] ERROR: Could not get disk usage, stopping pruning")
                break

            if current_usage <= prune_threshold_gib:
                log(f"[pruner] ✓ Usage now {current_usage} GiB - below threshold, stopping")
                break

            log(f"\n[pruner] Iteration {iteration + 1}/{prune_max}: Current usage {current_usage} GiB")

            # Get all delete category torrents via Qui
            resp = qui.get_torrents(instance_id, category=delete_cat)
            dels = resp.get("torrents") or []

            if not dels:
                log("[pruner] No more torrents in delete category")
                break

            # Filter for inactive torrents only
            inactive = [t for t in dels if t.get("dlspeed", 0) == 0 and t.get("upspeed", 0) == 0]

            if not inactive:
                log(f"[pruner] No inactive torrents available (all {len(dels)} have activity)")
                break

            log(f"[pruner] Found {len(inactive)} inactive torrents (out of {len(dels)} total)")

            # Sort by popularity (least popular first), take the least popular one
            inactive_sorted = sorted(inactive, key=lambda x: x.get("popularity", 0))
            to_delete = inactive_sorted[0]
            torrent_name = to_delete.get("name", "")
            torrent_hash = to_delete.get("hash")
            torrent_size_gib = to_delete.get("size", 0) / (1024**3)
            popularity = to_delete.get("popularity", 0)

            log(f"[pruner] Least popular inactive torrent:")
            log(f"[pruner]   Name: {torrent_name[:70]}")
            log(f"[pruner]   Size: {torrent_size_gib:.2f} GiB")
            log(f"[pruner]   Popularity: {popularity:.3f}")

            # Look up cross-seeds on the same instance
            cross_seeds = []
            try:
                all_matches = qui.get_cross_seed_matches(instance_id, torrent_hash)
                cross_seeds = [cs for cs in all_matches if cs.get("instance_id") == instance_id]
                skipped = len(all_matches) - len(cross_seeds)
                if cross_seeds:
                    log(f"[pruner]   Cross-seeds (same instance): {len(cross_seeds)}")
                    for cs in cross_seeds:
                        log(f"[pruner]     - {cs.get('name', '?')[:60]}")
                if skipped:
                    log(f"[pruner]   Skipping {skipped} cross-seed(s) on other instances")
            except Exception as e:
                log(f"[pruner]   Cross-seed lookup failed (will delete primary only): {e}")

            try:
                # Build deletion: primary hash + same-instance cross-seed hashes
                hashes = [torrent_hash]
                for cs in cross_seeds:
                    cs_hash = cs.get("hash")
                    if cs_hash:
                        hashes.append(cs_hash)

                log(f"[pruner] Deleting {len(hashes)} torrent(s)...")

                qui.delete_torrents(
                    instance_id,
                    hashes=hashes,
                    delete_files=True,
                )
                deleted_count += 1
                log(f"[pruner] ✓ Deleted ({deleted_count} total this run)")

                # Wait for deletion to complete and disk to update
                time.sleep(check_interval_sec)
            except Exception as e:
                log(f"[pruner] ✗ Failed to delete: {e}")
                break

    # Final summary
    final_usage = get_seedbox_usage_gib()
    if final_usage is not None:
        freed = initial_usage - final_usage
        log(f"\n[pruner] Summary:")
        log(f"[pruner]   Initial usage: {initial_usage} GiB")
        log(f"[pruner]   Final usage: {final_usage} GiB")
        log(f"[pruner]   Freed: ~{freed:.1f} GiB")
        log(f"[pruner]   Torrents deleted: {deleted_count}")

        if final_usage > prune_threshold_gib:
            log(f"[pruner] WARNING: Still above threshold ({prune_threshold_gib} GiB)")
            if deleted_count >= prune_max:
                log(f"[pruner]   Reached safety limit of {prune_max} deletions per run")
            else:
                log(f"[pruner]   No more inactive torrents available for deletion")
    else:
        log(f"\n[pruner] Summary: Deleted {deleted_count} torrents")

    log("\nseedbox_pruner done.")


if __name__ == "__main__":
    try:
        main()
    except KeyboardInterrupt:
        raise
    except SystemExit:
        raise
    except Exception as e:
        log(f"FATAL: {e}")
        import traceback
        traceback.print_exc()
        sys.exit(1)

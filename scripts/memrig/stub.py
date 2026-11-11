#!/usr/bin/env python3
"""Stub qBittorrent WebUI with a large, realistic library for a memory rig.

    python3 stub.py <port> <torrent count> [webapi version]

Unlike the field-test stub, maindata is a full update only for rid=0 and a
small delta afterwards, as the real WebUI does. Names repeat in groups to
look like a cross-seed library. Binds 127.0.0.1 only; all hosts are .test.
GET /_counts returns requests per path, with unknown paths marked "?".
"""
import hashlib
import json
import random
import sys
import threading
import time
from collections import Counter
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, urlparse

PORT = int(sys.argv[1])
COUNT = int(sys.argv[2])
API = sys.argv[3] if len(sys.argv) > 3 else "2.11.4"
NOW = int(time.time())
rnd = random.Random(7)

TRACKERS = ["https://tracker-%02d.test/announce/%032x" % (i, rnd.getrandbits(128)) for i in range(40)]
CATS = ["tv", "movies", "music", "books", "games", "tv-4k", "movies-4k", "cross-seed", "anime", "misc"]
TAGS = ["cross-seed", "sonarr", "radarr", "upgraded", "freeleech", "keep", "noHL", "tracker-issue"]
WORDS = "Silent Harbor Crimson Ledger Northern Vale Paper Lantern Hollow Crown Iron Orchard Glass Tide Amber Signal".split()

torrents = {}
order = []
i = 0
content = 0
while i < COUNT:
    # each piece of content is seeded on 1 to 4 trackers
    copies = min(rnd.choice([1, 1, 2, 3, 4]), COUNT - i)
    title = ".".join(rnd.sample(WORDS, 3))
    name = "%s.%d.S%02dE%02d.1080p.WEB-DL.DDP5.1.H.264-GRP%d" % (title, content, content % 9 + 1, content % 24 + 1, content % 50)
    size = rnd.randrange(200_000_000, 60_000_000_000)
    cat = rnd.choice(CATS)
    for c in range(copies):
        h = hashlib.sha1(b"%d" % i).hexdigest()
        tr = TRACKERS[(content + c * 7) % len(TRACKERS)]
        save = "/data/torrents/%s" % cat
        added = NOW - i * 600
        torrents[h] = {
            "added_on": added, "amount_left": 0, "auto_tmm": c == 0, "availability": -1,
            "category": cat, "comment": "https://tracker.test/torrents.php?id=%d" % i,
            "completed": size, "completion_on": added + 900,
            "content_path": "%s/%s" % (save, name), "dl_limit": 0, "dlspeed": 0,
            "download_path": "", "downloaded": size, "downloaded_session": 0, "eta": 8640000,
            "f_l_piece_prio": False, "force_start": False, "has_metadata": True,
            "inactive_seeding_time_limit": -2, "infohash_v1": h, "infohash_v2": "",
            "last_activity": NOW - rnd.randrange(86400), "magnet_uri": "magnet:?xt=urn:btih:%s&dn=%s&tr=%s" % (h, name, tr),
            "max_inactive_seeding_time": -1, "max_ratio": -1, "max_seeding_time": -1,
            "name": name, "num_complete": rnd.randrange(200), "num_incomplete": rnd.randrange(5),
            "num_leechs": 0, "num_seeds": 0, "popularity": 0.0, "priority": 0, "private": True,
            "progress": 1, "ratio": 1.5, "ratio_limit": -2, "reannounce": rnd.randrange(3600),
            "root_path": "%s/%s" % (save, name), "save_path": save, "seeding_time": NOW - added,
            "seeding_time_limit": -2, "seen_complete": NOW - 3600, "seq_dl": False, "size": size,
            "state": rnd.choice(["stalledUP"] * 8 + ["uploading", "pausedUP"]), "super_seeding": False,
            "tags": ", ".join(sorted(rnd.sample(TAGS, rnd.randrange(3)))), "time_active": NOW - added,
            "total_size": size, "tracker": tr, "trackers_count": 1, "up_limit": 0,
            "uploaded": int(size * 1.5), "uploaded_session": 0, "upspeed": 0,
        }
        order.append(h)
        i += 1
    content += 1

tracker_map = {}
for h, t in torrents.items():
    tracker_map.setdefault(t["tracker"], []).append(h)

server_state = {"dl_info_speed": 0, "up_info_speed": 4096, "alltime_dl": 10**13, "alltime_ul": 10**14,
                "free_space_on_disk": 10**12, "connection_status": "connected", "dht_nodes": 0,
                "global_ratio": "10.0", "total_peer_connections": 50, "queueing": False,
                "use_alt_speed_limits": False, "refresh_interval": 1500}
categories = {c: {"name": c, "savePath": "/data/torrents/%s" % c} for c in CATS}

lock = threading.Lock()
rid = 0
counts = Counter()
FULL = None


def tracker_rows(t):
    rows = [{"url": u, "status": 0, "tier": -1, "num_peers": 0, "num_seeds": 0, "num_leeches": 0,
             "num_downloaded": 0, "msg": "This torrent is private"} for u in ("** [DHT] **", "** [PeX] **", "** [LSD] **")]
    rows.append({"url": t["tracker"], "status": 2, "tier": 0, "num_peers": 3, "num_seeds": t["num_complete"],
                 "num_leeches": t["num_incomplete"], "num_downloaded": 12, "msg": ""})
    return rows


def files(h):
    t = torrents.get(h)
    if t is None:
        return []
    n = int(h[:2], 16) % 12 + 1
    return [{"index": k, "name": "%s/%s.part%02d.mkv" % (t["name"], t["name"], k), "size": t["size"] // n,
             "progress": 1, "priority": 1, "is_seed": True, "piece_range": [k * 100, k * 100 + 99],
             "availability": 1} for k in range(n)]


def maindata(req_rid):
    global rid, FULL
    with lock:
        rid += 1
        if req_rid == 0:
            if FULL is None:
                FULL = {"full_update": True, "torrents": torrents, "categories": categories, "tags": TAGS,
                        "trackers": tracker_map, "server_state": server_state}
            FULL["rid"] = rid
            return json.dumps(FULL).encode()
        changed = {}
        for h in rnd.sample(order, min(len(order), max(1, COUNT // 200))):
            t = torrents[h]
            t["upspeed"] = rnd.randrange(500_000)
            t["uploaded"] += t["upspeed"]
            t["last_activity"] = int(time.time())
            changed[h] = {"upspeed": t["upspeed"], "uploaded": t["uploaded"], "last_activity": t["last_activity"]}
        return json.dumps({"rid": rid, "torrents": changed,
                           "server_state": {"up_info_speed": rnd.randrange(10**7)}}).encode()


STATIC = {
    "/api/v2/app/webapiVersion": API.encode(),
    "/api/v2/app/version": b"v5.1.2",
    "/api/v2/app/defaultSavePath": b"/data/torrents",
    "/api/v2/app/buildInfo": b'{"qt":"6.7","libtorrent":"2.0.11","boost":"1.86","openssl":"3.0","bitness":64}',
    "/api/v2/app/preferences": b'{"save_path":"/data/torrents","web_ui_username":"admin","queueing_enabled":false}',
    "/api/v2/torrents/categories": json.dumps(categories).encode(),
    "/api/v2/torrents/tags": json.dumps(TAGS).encode(),
    "/api/v2/sync/torrentPeers": b'{"peers":{},"rid":1,"full_update":true}',
    "/api/v2/transfer/info": b'{"dl_info_speed":0,"up_info_speed":4096,"connection_status":"connected"}',
}


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *args):
        pass

    def send(self, body, extra=()):
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        for k, v in extra:
            self.send_header(k, v)
        self.end_headers()
        self.wfile.write(body)

    def params(self):
        q = parse_qs(urlparse(self.path).query)
        n = int(self.headers.get("Content-Length") or 0)
        if n:
            q.update(parse_qs(self.rfile.read(n).decode("utf-8", "replace")))
        return {k: v[0] for k, v in q.items()}

    def do_POST(self):
        self.do_GET()

    def do_GET(self):
        path = urlparse(self.path).path
        p = self.params()
        if path == "/_counts":
            self.send(json.dumps(counts, indent=1).encode())
            return
        counts[path] += 1
        if path == "/api/v2/auth/login":
            self.send(b"Ok.", [("Set-Cookie", "SID=stub; Path=/")])
        elif path == "/api/v2/sync/maindata":
            self.send(maindata(int(p.get("rid", 0))))
        elif path == "/api/v2/torrents/info":
            hashes = p.get("hashes")
            keys = hashes.split("|") if hashes else order
            inc = p.get("includeTrackers") == "true"
            out = []
            for h in keys:
                t = torrents.get(h)
                if t is None:
                    continue
                row = dict(t, hash=h)
                if inc:
                    row["trackers"] = tracker_rows(t)
                out.append(row)
            self.send(json.dumps(out).encode())
        elif path == "/api/v2/torrents/trackers":
            t = torrents.get(p.get("hash", ""))
            self.send(json.dumps(tracker_rows(t) if t else []).encode())
        elif path == "/api/v2/torrents/files":
            self.send(json.dumps(files(p.get("hash", ""))).encode())
        elif path == "/api/v2/torrents/properties":
            t = torrents.get(p.get("hash", ""), {})
            self.send(json.dumps({"save_path": t.get("save_path", ""), "total_size": t.get("size", 0),
                                  "piece_size": 4194304, "pieces_num": 1000, "is_private": True,
                                  "comment": t.get("comment", ""), "created_by": "stub"}).encode())
        elif path == "/api/v2/torrents/export":
            name = torrents.get(p.get("hash", ""), {}).get("name", "x").encode()
            pieces = b"\0" * 20 * 2000
            self.send(b"d8:announce31:https://tracker-00.test/announce4:infod6:lengthi1000e4:name%d:%s12:piece lengthi4194304e6:pieces%d:%se"
                      b"e" % (len(name), name, len(pieces), pieces))
        elif path in STATIC:
            self.send(STATIC[path])
        else:
            counts["? " + path] += 1
            self.send(b"[]")


class Server(ThreadingHTTPServer):
    daemon_threads = True
    allow_reuse_address = True
    request_queue_size = 256


Server(("127.0.0.1", PORT), Handler).serve_forever()

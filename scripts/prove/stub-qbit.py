#!/usr/bin/env python3
"""Stub qBittorrent WebUI for a scenario. Starts with an empty library.

    python3 stub-qbit.py <port>

POST /_seed with {"torrents": [{"name": ..., "category": ..., "tags": ...}],
"categories": ["anime", ...], "tags": [...]} adds made-up torrents and categories.
A torrent gets defaults for each field it leaves out; its hash is "%040x" % n.
GET /_calls returns each write call that qui sent, in order, as
[{"path": ..., "form": {...}}]. setCategory, createCategory and addTags also
change the library, so the UI shows the result.
Binds 127.0.0.1 only. Copied from the field-test skill (stub-qbit.py) and cut down.
"""
import json
import sys
import time
from http.server import BaseHTTPRequestHandler, HTTPServer
from urllib.parse import parse_qsl, urlparse

PORT = int(sys.argv[1])
NOW = int(time.time())
TRACKER = "https://tracker.example.invalid/announce"

torrents = {}
categories = {}
tags = []
calls = []
rid = 0

STATIC = {
    "/api/v2/app/webapiVersion": b"2.11.2",
    "/api/v2/app/version": b"v5.0.4",
    "/api/v2/app/defaultSavePath": b"/downloads",
    "/api/v2/app/buildInfo": b'{"qt":"6.7","libtorrent":"2.0.10","boost":"1.86","openssl":"3.0","bitness":64}',
    "/api/v2/app/preferences": b'{"save_path":"/downloads","web_ui_username":"admin"}',
    "/api/v2/torrents/properties": b"{}",
    "/api/v2/sync/torrentPeers": b'{"peers":{}}',
    "/api/v2/transfer/info": b"{}",
}
READS = {"/api/v2/sync/maindata", "/api/v2/torrents/info", "/api/v2/torrents/categories",
         "/api/v2/torrents/tags", "/api/v2/auth/login"} | set(STATIC)


def add_torrent(fields):
    n = len(torrents) + 1
    h = "%040x" % n
    torrents[h] = {
        "name": "Made.Up.Release.%d.1080p.WEB-GRP" % n, "infohash_v1": h, "infohash_v2": "",
        "state": "uploading", "added_on": NOW - n * 60, "completion_on": NOW - n * 30,
        "size": 1_000_000_000, "total_size": 1_000_000_000, "progress": 1,
        "category": "", "tags": "", "tracker": TRACKER,
        "save_path": "/downloads", "content_path": "/downloads/x%d" % n,
        "ratio": 1.5, "uploaded": 1_500_000_000, "downloaded": 1_000_000_000,
        "dlspeed": 0, "upspeed": 1024, "num_seeds": 1, "num_leechs": 0,
        "eta": 8640000, "priority": 0, "last_activity": NOW, "amount_left": 0,
        **fields,
    }


def add_category(name):
    categories.setdefault(name, {"name": name, "savePath": ""})


def apply_write(path, form):
    hashes = form.get("hashes", "")
    targets = list(torrents) if hashes == "all" else [h for h in hashes.split("|") if h in torrents]
    if path == "/api/v2/torrents/createCategory":
        add_category(form.get("category", ""))
    elif path == "/api/v2/torrents/setCategory":
        for h in targets:
            torrents[h]["category"] = form.get("category", "")
    elif path == "/api/v2/torrents/addTags":
        for h in targets:
            have = [t for t in torrents[h]["tags"].split(", ") if t]
            torrents[h]["tags"] = ", ".join(have + [t for t in form.get("tags", "").split(",") if t and t not in have])


def maindata():
    global rid
    rid += 1
    for t in torrents.values():
        t["upspeed"] += 1  # each delta carries rows, as on a busy client
    return json.dumps({
        "rid": rid, "full_update": True, "torrents": torrents, "categories": categories, "tags": tags,
        "trackers": {TRACKER: list(torrents)}, "server_state": {
            "dl_info_speed": 0, "up_info_speed": 4096, "alltime_dl": 1, "alltime_ul": 1,
            "free_space_on_disk": 10**12, "connection_status": "connected", "dht_nodes": 100},
    }).encode()


class Handler(BaseHTTPRequestHandler):
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

    def do_POST(self):
        raw = self.rfile.read(int(self.headers.get("Content-Length") or 0)).decode()
        path = urlparse(self.path).path
        if path == "/_seed":
            seed = json.loads(raw)
            for name in seed.get("categories", []):
                add_category(name)
            tags.extend(seed.get("tags", []))
            for t in seed.get("torrents", []):
                add_torrent(t)
            self.send(json.dumps(list(torrents)).encode())
            return
        if path not in READS:
            form = dict(parse_qsl(raw, keep_blank_values=True))
            calls.append({"path": path, "form": form})
            apply_write(path, form)
        self.reply(path)

    def do_GET(self):
        url = urlparse(self.path)
        if url.path == "/_calls":
            self.send(json.dumps(calls).encode())
            return
        self.reply(url.path)

    def reply(self, path):
        if path == "/api/v2/auth/login":
            self.send(b"Ok.", [("Set-Cookie", "SID=stub; Path=/")])
        elif path == "/api/v2/sync/maindata":
            self.send(maindata())
        elif path == "/api/v2/torrents/info":
            self.send(json.dumps([dict(t, hash=h) for h, t in torrents.items()]).encode())
        elif path == "/api/v2/torrents/categories":
            self.send(json.dumps(categories).encode())
        elif path == "/api/v2/torrents/tags":
            self.send(json.dumps(tags).encode())
        else:
            self.send(STATIC.get(path, b"[]"))


HTTPServer(("127.0.0.1", PORT), Handler).serve_forever()

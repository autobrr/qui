// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package peerclienticons

const (
	svg = "image/svg+xml"
	png = "image/png"
)

// Icon is one logo that qui downloads at runtime. qui ships the address and
// the hash, never the file: see docs/adr/0037.
type Icon struct {
	Name        string
	URL         string // raw URL pinned to a full commit SHA
	SHA256      string // hex hash of the file at URL
	ContentType string
	Fill        string // optional #rrggbb added to the root <svg> element
}

// Icons is the list that the server serves. A new hash makes each server
// download the file again, because the file name on disk contains the hash.
var Icons = []Icon{
	{"qbittorrent", "https://raw.githubusercontent.com/qbittorrent/qBittorrent/62259b84b333ed5d8db106199db72dbe90ea90c1/src/icons/qbittorrent-tray.svg", "371d532463d91677316853c807f35991e8140b3cd8f6fe25ced10eeef7ee035d", svg, ""},
	{"transmission", "https://raw.githubusercontent.com/transmission/transmission/f4bd5f3457198d6d88e2acde3b2a96eaa5cc84df/icons/hicolor_apps_scalable_transmission.svg", "3e4d67e348c5f99498bcdfdd7608ec3e4a6d42a57a7418ca25c4b6348a21093a", svg, ""},
	{"deluge", "https://raw.githubusercontent.com/deluge-torrent/deluge/e58075416dedd53636e89b1cd240f86f2e7c2ee0/deluge/ui/data/pixmaps/deluge.svg", "67ab9d5c03f646109bc6c2f15bb95bf36350dc89ce90d6ac5ec680bc3becf1db", svg, ""},
	{"rutorrent", "https://raw.githubusercontent.com/Novik/ruTorrent/f707df7a1f4c68e92a2a7b44fec4da6f4d9a4765/images/favicon-96x96.png", "9ea1944c00d9127f8d5b84d157e6fda74ef2176f85055812e4bce9e142db028c", png, ""},
	{"libtorrent", "https://raw.githubusercontent.com/arvidn/libtorrent/a1f7c90b3c6810602a07b3c99d3634c7e91cf819/docs/img/logo-color.svg", "da255a53390bee6bfd55ca0b931f7b1a5cbeb4f0b6f3c84dc59199b8833c28ae", svg, ""},
	{"porla", "https://raw.githubusercontent.com/porla/porla/6b3f2670250384597a5d79eae1f312fb211a7695/.github/images/logo.svg", "dd58a3afd3e03a69d9fc1343f9da0a211b91a8b6f4d2fb9141d0e1e9478fad80", svg, ""},
	{"biglybt", "https://raw.githubusercontent.com/BiglySoftware/BiglyBT/35fbc1ebf56fd69acc3f72b8f51270a9d46f9e3e/assets/linux/biglybt.svg", "82da7ec2e5b30e6409461c48b5e916b55957c5c5bdb06c99be8875f3108e3984", svg, ""},
	{"webtorrent", "https://raw.githubusercontent.com/webtorrent/webtorrent.io/58abb9ee72a59aa7f303386314b8c16940b26819/static/img/WebTorrent.svg", "29c3fab76e6dbc5b2cde3f889936e693dafbb5a9ffc3092454df210bfc0e457f", svg, ""},
	{"picotorrent", "https://raw.githubusercontent.com/picotorrent/picotorrent/24e3e869470e2ffc9f0199e4972ba035d3581847/docs/src/static/logo.png", "de3ff8f112e3be67c1538d8fc2bd7eb4d35ca90df582fe1f61a969677d405b17", png, ""},
	{"libretorrent", "https://raw.githubusercontent.com/proninyaroslav/libretorrent/ae96f20b9852d2140fd704a3f2451ed7679e35a0/art/icon.svg", "39bdea586e78d5cabeb2926d61de568348412989f09ebee76d712e61ed7d7c85", svg, ""},
	{"ktorrent", "https://invent.kde.org/frameworks/breeze-icons/-/raw/e3c7154b4f243413b0cb9028484977f3c37de15c/icons/apps/48/ktorrent.svg", "7f2042bd9900b5c6370a9d9093dc286fbce9b77fe32a342e918adae6bafff3f2", svg, ""},
	// Simple Icons glyphs are black paths; Fill gives them the brand color from simple-icons.json.
	{"utorrent", "https://raw.githubusercontent.com/simple-icons/simple-icons/90646e4bf7f3b89df6c37e65baf3e7bc759a517c/icons/utorrent.svg", "dd0eab3000878a6d34952f49f0bc3968ab80d22f0685a7a0ff4d497154c42dbf", svg, "#76b83f"},
	{"bittorrent", "https://raw.githubusercontent.com/simple-icons/simple-icons/90646e4bf7f3b89df6c37e65baf3e7bc759a517c/icons/bittorrent.svg", "8030fbd63e69f8cf5e5671e77bff1520391dd6dfa09a954b9c9d9807199330e7", svg, "#050505"},
	{"bitcomet", "https://raw.githubusercontent.com/simple-icons/simple-icons/90646e4bf7f3b89df6c37e65baf3e7bc759a517c/icons/bitcomet.svg", "7c8d1a7242594cc618603b5cc57952530535cc4a61f4029abf3e9c43597d5b80", svg, "#f49923"},
}

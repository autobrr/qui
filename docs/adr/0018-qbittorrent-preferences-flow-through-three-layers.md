---
status: accepted
date: 2026-10-16
---

# qBittorrent preferences flow through three layers

qui shows qBittorrent preferences through three layers, and each layer owns one kind of knowledge. go-qbittorrent owns the wire: one `AppPreferences` field for each key from qBittorrent 4.3.9 on, a record of which keys qBittorrent sent, and accessors that hide renamed keys. A preference catalog in the qui backend owns the display: the tab, the section, the control, the unit, the libtorrent and platform gates, and whether a user can change the key. The catalog is also the only code that sends a preference write. The frontend renders the catalog view and owns only the widgets and the labels. A field shows only when qBittorrent sent its key and the gates pass, so qui keeps no version tables. The torrent list and its stream do not carry preferences.

We rejected three alternatives. A raw passthrough of the qBittorrent response puts no wire knowledge in go-qbittorrent, but the backend callers that read single keys then lose the rename accessors and the typed values. A schema in the frontend cannot reject a bad write on the server, and its drift test must compare TypeScript with Go. A catalog in go-qbittorrent puts UI layout into a library that autobrr also uses.

Three tests keep the layers in step. go-qbittorrent decodes a captured `app/preferences` response for each qBittorrent minor version and fails on a key without a field. qui fails on a go-qbittorrent key without a catalog entry. The frontend fails on a catalog key without a label. A `forbidigo` rule blocks preference writes outside the catalog package.

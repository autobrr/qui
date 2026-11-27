---
status: accepted
date: 2026-11-27
---

# One module hides secrets by the Audience of the reader, and every JSON response leaves through it

ADR 0040 says which Permission shows a tracker passkey and the URL of a notification target. ADR 0087 adds a test that reads each route with a read-only key. Between the two, each handler applied the rule itself: it asked the Permission question, chose the Hidden form, copied the value when it came from a cache, and put the full value back when a client sent the Hidden form. Twenty call sites in eleven files asked the question, and four pull requests in a row added more.

`internal/secrets` now owns this. The package that owns a type declares a Secret shape for it, once, as a typed value in a file named `secret_shapes.go`. One file, in `internal/api/secretshapes`, builds the encoder from those values. A handler returns the full value. `RespondJSON` takes the request, reads its Audience with `middleware.AudienceOf`, and the encoder applies each Secret shape that the Audience does not include. A stream encodes its payload through the same encoder, once for each Audience among its readers. An RSS Stand-in that a client sends back goes through `secrets.Restore`, which has one rule for no match and for more than one match.

The Audience of a request with no API key is a browser session, and it sees each Secret kind. The zero Audience sees none, so code that forgets to set one hides too much.

ADR 0048 stays as it is. The rows of the torrent list are still hidden in the qBittorrent package, and the Torrent view carries the Audience in place of the passkey access flag. The response is marked as hidden, and the encoder does not hide it again, so no passkey step moves to a caller and no list option travels in the request context.

The encoder is `encoding/json/v2` with the options of `encoding/json`, so a response keeps the bytes of `encoding/json`. The Audience reaches the encoder as an option of the call: each Audience has its own set of marshal functions, and the set of a browser session holds none. A Secret shape of one kind encodes its Hidden form with the set of the Audience plus that kind, so no Shape of that kind runs below it, and each value is hidden once.

## Considered options

- **A secret type that masks itself in `MarshalJSON`.** ADR 0087 rejected this as the check, because a JSON method cannot see the Permission of the request. That reason holds, and this decision uses no such method: the Audience reaches the encoder as an option of the call. A type of its own was also rejected as the mechanism: the fields are on go-qbittorrent types, so each row would need a copy into a second type for every reader, also for a browser session.
- **A declaration on each route**, next to the Permission of ADR 0040, that lists the JSON paths to hide. Rejected: it puts a second copy of each response shape in the route table, far from the type, and most routes would declare "none". It also does nothing for a stream, for the filter expression, or for a value that a client sends back.
- **An Audience parameter on each method that returns a secret.** Rejected as the main mechanism: it changes many signatures, and qui's own callers would all pass "everything". It stays in the one place where the source must know the reader: the torrent list, whose rows the stream compares and the filter expression reads.
- **A call that registers a Secret shape when its package starts.** Rejected: the order of package start decides whether the encoder sees a Shape, a test binary that does not link the owner package encodes the type with no Shape, a lint rule must then say where the call may stand, and a `Hidden` for a type with no Shape can only fail at run time. A typed value and one list have none of these faults.
- **A function in each handler that hides its own value, with the module only for the Permission table and the Hidden forms.** Rejected as the main mechanism: a new route can forget the call, and only the walk can object. It stays for the two values that no type describes, as a RawShape.
- **The rows of the torrent list hidden in the qBittorrent package and again at the exit.** Rejected: no Hidden form is safe to apply twice, and a reviewer measured the second pass at 63% to 79% more time for a key without `Torrents: write`. The same reason gives the rule for a Shape inside a Shape: the outer one hides, and the encoder runs no Shape of that kind below it.
- **Keep the handlers as they are and extend the test of ADR 0087.** Rejected: the test finds a missing hide only after it is written and only where the rig plants a value. It does not reduce the code that each new family adds.

## Consequences

- A new field that holds a secret needs one line in a Secret shape and one planted value in the rig of ADR 0087. `TestEveryShapeHidesEveryString` fails for a new text field on a type that has a Secret shape until its owner hides it or lists it as plain.
- A new type with a secret needs a Shape in its package and one line in the list of that package.
- forbidigo stops a handler package from writing JSON past the module, from reading a secret field, and from building an Audience or an encoder.
- Neither the compiler nor forbidigo sees a call through an interface, a secret that was copied into a plain string, a new type with no Secret shape, or a type that embeds one with a Secret shape. The walk of ADR 0087 stays the check for those.
- A Secret shape does not run for a type that embeds the type it names, so `TorrentView` has its own.
- The writer encodes into a buffer before it writes the header, so a value that cannot be encoded gets a 500 with the body `{"error":"Failed to encode response"}` and no part of the value.
- A change to the Preference `add_trackers` or `add_trackers_url` of qBittorrent needs `Torrents: write`. The step that moves the tracker lines of the Preferences adds this to ADR 0040.
- The placeholder of a stored credential and the empty password of the preference catalog stay outside this module.

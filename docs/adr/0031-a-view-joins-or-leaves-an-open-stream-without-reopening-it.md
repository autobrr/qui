---
status: accepted
date: 2026-10-27
---

# A view joins or leaves an open stream without reopening it

A click on a torrent added a details subscription, and the subscription set lived in the EventSource URL. Each change closed the shared stream and opened a new one. The server then sent the init snapshot of every subscription again, the main table included. On a 5,600-torrent instance, one switch between two torrents sent about 1 MB.

A view now joins or leaves an open stream with a request beside the stream. The stream library fixes the topics of a connection when it opens, so the frames of a subscription that joins later travel on the topic of the connection. qui already gives each connection a topic for activity events. If the request fails, the browser reopens the stream as before.

## Considered options

- A second EventSource for the details row. Rejected: behind a proxy that uses HTTP/1.1, each browser tab holds two connections, and the browser allows 6 for each host.
- Use the main table row when the torrent is on the loaded page. Rejected: a torrent off the page still reopens the stream, and other small views keep the problem.

## Consequences

The stream protocol has a request beside the one-way stream. The server keeps a map from connection to subscriptions. The demo must answer the request.

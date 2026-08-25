---
sidebar_position: 1
title: Metrics
---

# Prometheus metrics

Qui can expose Prometheus metrics for its process and qBittorrent instances. The metrics server uses a separate port, which defaults to `9074`.

The server has no authentication unless you configure basic authentication. Bind it to a private interface or protect it before you expose it to a network.

## Enable metrics

Metrics are disabled by default. Enable them in the configuration file or with environment variables.

### Configuration file

```toml
metricsEnabled = true
metricsHost = "127.0.0.1"
metricsPort = 9074
# metricsBasicAuthUsers = "user:password"
```

### Environment variables

```bash
QUI__METRICS_ENABLED=true
QUI__METRICS_HOST=127.0.0.1
QUI__METRICS_PORT=9074
# QUI__METRICS_BASIC_AUTH_USERS="user:password"
```

Qui accepts a comma-separated list of `user:password` entries. Passwords are plaintext and can contain colons. Usernames cannot contain colons. Commas cannot appear in usernames or passwords.

Protect the configuration file or environment that contains the passwords.

## Configure Prometheus

Add Qui to your Prometheus scrape configuration:

```yaml
scrape_configs:
  - job_name: qui
    static_configs:
      - targets: ["localhost:9074"]
    metrics_path: /metrics
    scrape_interval: 30s
    # basic_auth:
    #   username: prometheus
    #   password: yourpassword
```

## Qui metrics

The qBittorrent metrics use the `instance_id` and `instance_name` labels. Tracker metrics also use `tracker_name`.

| Metric | Type | Labels | Value and reset behavior |
|---|---|---|---|
| `qbittorrent_torrents_downloading` | Gauge | `instance_id`, `instance_name` | Current downloading torrent count. |
| `qbittorrent_torrents_seeding` | Gauge | `instance_id`, `instance_name` | Current seeding torrent count. |
| `qbittorrent_torrents_paused` | Gauge | `instance_id`, `instance_name` | Current paused or stopped torrent count. |
| `qbittorrent_torrents_error` | Gauge | `instance_id`, `instance_name` | Current torrent error count. |
| `qbittorrent_torrents_checking` | Gauge | `instance_id`, `instance_name` | Current checking torrent count. |
| `qbittorrent_session_download_bytes` | Counter | `instance_id`, `instance_name` | Bytes downloaded during the current qBittorrent session. A qBittorrent restart resets this value. |
| `qbittorrent_session_upload_bytes` | Counter | `instance_id`, `instance_name` | Bytes uploaded during the current qBittorrent session. A qBittorrent restart resets this value. |
| `qbittorrent_alltime_download_bytes` | Counter | `instance_id`, `instance_name` | All-time bytes downloaded by qBittorrent. This value persists across restarts. Clearing qBittorrent's saved statistics resets it. |
| `qbittorrent_alltime_upload_bytes` | Counter | `instance_id`, `instance_name` | All-time bytes uploaded by qBittorrent. This value persists across restarts. Clearing qBittorrent's saved statistics resets it. |
| `qbittorrent_instance_connection_status` | Gauge | `instance_id`, `instance_name` | `1` for an active, healthy connection. `0` for a disabled or unhealthy instance. |
| `qbittorrent_scrape_errors_total` | Counter | `instance_id`, `instance_name`, `type` | A value of `1` for each failed collection sample. A successful collection omits the series. |
| `qbittorrent_tracker_torrents` | Gauge | `instance_id`, `instance_name`, `tracker_name` | Current torrent count for the tracker group. |
| `qbittorrent_tracker_uploaded_bytes` | Gauge | `instance_id`, `instance_name`, `tracker_name` | Uploaded bytes from current torrents in the tracker group. |
| `qbittorrent_tracker_downloaded_bytes` | Gauge | `instance_id`, `instance_name`, `tracker_name` | Downloaded bytes from current torrents in the tracker group. |
| `qbittorrent_tracker_total_size_bytes` | Gauge | `instance_id`, `instance_name`, `tracker_name` | Current content size for the tracker group. Qui counts a shared content path once within each group. |
| `qui_db_wedged_transaction_total` | Counter | None | SQLite nested-transaction detections since Qui started. A Qui restart resets this value. |

Qui also exports the standard `go_*` and `process_*` metrics from the Prometheus Go client. Those series depend on the Go and client-library versions.

Disabled and disconnected instances expose only `qbittorrent_instance_connection_status`. Qui omits their torrent and transfer metrics.

## Tracker metric limits

Tracker metrics describe torrents that remain in qBittorrent. Qui does not store tracker history. When you remove a torrent, its transfer totals leave the tracker metrics.

Qui assigns a torrent's full transfer totals to each tracker group associated with that torrent. Do not sum tracker groups to calculate unique instance traffic.

The `tracker_name` label uses the customization display name when one is configured. Otherwise, it uses the tracker domain. Included secondary domains contribute to their configured group.

## PromQL examples

### Tracker totals

Uploaded bytes for the torrents that are currently present:

```promql
sum by (instance_name, tracker_name) (
  qbittorrent_tracker_uploaded_bytes
)
```

Downloaded bytes for the torrents that are currently present:

```promql
sum by (instance_name, tracker_name) (
  qbittorrent_tracker_downloaded_bytes
)
```

### Instance transfer rates

The session metrics are counters. `rate()` handles a qBittorrent restart and returns the average bytes per second.

```promql
sum by (instance_name) (
  rate(qbittorrent_session_upload_bytes[5m])
)
```

```promql
sum by (instance_name) (
  rate(qbittorrent_session_download_bytes[5m])
)
```

Grafana users can replace `[5m]` with `[$__rate_interval]`.

### Estimated tracker transfer rates

Tracker byte metrics are gauges because their values decrease when torrents leave the library. Use `deriv()` for an estimate:

```promql
sum by (instance_name, tracker_name) (
  clamp_min(deriv(qbittorrent_tracker_uploaded_bytes[5m]), 0)
)
```

A torrent removal can hide traffic until the removal leaves the selected time range.

### Collection errors

The error collector emits one sample for each failed collection. Count those samples over time:

```promql
sum by (instance_name, type) (
  sum_over_time(qbittorrent_scrape_errors_total[5m])
)
```

Do not use `rate()` or `increase()` with this metric. It does not retain a cumulative count between scrapes.

### Disconnected instances

```promql
qbittorrent_instance_connection_status == 0
```

This query includes disabled instances. Monitor Prometheus's `up{job="qui"}` metric separately because Qui cannot report an unreachable metrics endpoint.

## Grafana dashboard

[Download the Qui Grafana dashboard](/examples/qui-grafana-dashboard.json), then import the JSON file in Grafana. Select the Prometheus data source that scrapes Qui.

The dashboard has an instance filter and four panels:

- qBittorrent connection status
- session upload and download rates
- current tracker upload and download totals
- collection errors during the last five minutes

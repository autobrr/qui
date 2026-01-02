# qui

A fast, modern web interface for qBittorrent. Supports managing multiple qBittorrent instances from a single, lightweight application.

<div align="center">
  <img src=".github/assets/qui.png" alt="qui" width="100%" />
</div>

## Documentation

Full documentation available at **[getqui.com](https://getqui.com)**

## Quick Start

### Linux x86_64

```bash
# Download and extract the latest release
wget $(curl -s https://api.github.com/repos/autobrr/qui/releases/latest | grep browser_download_url | grep linux_x86_64 | cut -d\" -f4)
tar -C /usr/local/bin -xzf qui*.tar.gz

# Run
./qui serve
```

The web interface will be available at http://localhost:7476

### Docker

```bash
docker run -d \
  -p 7476:7476 \
  -v $(pwd)/config:/config \
  ghcr.io/autobrr/qui:latest
```

## Features

- **Single Binary**: No dependencies, just download and run
- **Multi-Instance Support**: Manage all your qBittorrent instances from one place
- **Fast & Responsive**: Optimized for performance with large torrent collections
- **Cross-Seed**: Automatically find and add matching torrents across trackers
- **Automations**: Rule-based torrent management with conditions and actions
- **Backups & Restore**: Scheduled snapshots with multiple restore modes
- **Reverse Proxy**: Transparent qBittorrent proxy for external apps

## Community

Join our community on [Discord](https://discord.autobrr.com/qui)!

## Support

- [GitHub Issues](https://github.com/autobrr/qui/issues)

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

GPL-2.0-or-later

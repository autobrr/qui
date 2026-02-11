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

- [GitHub Discussions](https://github.com/autobrr/qui/discussions/new/choose) - Feature requests and bug reports
- [GitHub Issues](https://github.com/autobrr/qui/issues) - Work in progress

## Support Development

qui is developed and maintained by volunteers. Your support helps us continue improving the project.

### Premium Themes

Purchase premium themes directly from Settings → Themes in your qui instance. Your license key is delivered instantly after checkout.

### Donations

If you'd like to support development beyond theme purchases, donations are always appreciated.

- **soup**
  - [Patreon](https://www.patreon.com/c/s0up4200)
  - [GitHub Sponsors](https://github.com/sponsors/s0up4200)
  - [Buy Me a Coffee](https://buymeacoffee.com/s0up4200)
  - [Ko-fi](https://ko-fi.com/s0up4200)
- **zze0s**
  - [GitHub Sponsors](https://github.com/sponsors/zze0s)
  - [Buy Me a Coffee](https://buymeacoffee.com/ze0s)

#### Cryptocurrency

#### Bitcoin (BTC)
- soup: `bc1qfe093kmhvsa436v4ksz0udfcggg3vtnm2tjgem`
- zze0s: `bc1q2nvdd83hrzelqn4vyjm8tvjwmsuuxsdlg4ws7x`

#### Ethereum (ETH)
- soup: `0xD8f517c395a68FEa8d19832398d4dA7b45cbc38F`
- zze0s: `0xBF7d749574aabF17fC35b27232892d3F0ff4D423`

#### Litecoin (LTC)
- soup: `ltc1q86nx64mu2j22psj378amm58ghvy4c9dw80z88h`
- zze0s: `ltc1qza9ffjr5y43uk8nj9ndjx9hkj0ph3rhur6wudn`

#### Monero (XMR)
- soup: `8AMPTPgjmLG9armLBvRA8NMZqPWuNT4US3kQoZrxDDVSU21kpYpFr1UCWmmtcBKGsvDCFA3KTphGXExWb3aHEu67JkcjAvC`
- zze0s: `44AvbWXzFN3bnv2oj92AmEaR26PQf5Ys4W155zw3frvEJf2s4g325bk4tRBgH7umSVMhk88vkU3gw9cDvuCSHgpRPsuWVJp`

---

For other currencies or donation methods, [reach out on Discord](https://discord.autobrr.com/qui).

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

GPL-2.0-or-later

# Website

This website is built using [Docusaurus](https://docusaurus.io/), a modern static website generator.

## Installation

```bash
pnpm install
```

## Local Development

```bash
pnpm start
```

This command starts a local development server and opens up a browser window. Most changes are reflected live without having to restart the server.

## Build

```bash
pnpm build
```

This command generates static content into the `build` directory.

## Deployment

Netlify builds [getqui.com](https://getqui.com) from the `main` branch. A docs change merged to `develop` goes live when it reaches `main`: with the next release, or earlier as a cherry-pick of the squashed commit onto a branch from `main`.

The Netlify configuration in [`netlify.toml`](netlify.toml) builds the web demo into `static/demo`, then runs `pnpm build`. The publish directory is `build`.

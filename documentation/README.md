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

Documentation is deployed to [getqui.com](https://getqui.com) via Netlify.

The Netlify configuration in [`netlify.toml`](netlify.toml) sets `pnpm build` as the build command and `build` as the publish directory.

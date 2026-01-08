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

**Automatic deployment**: Pushes to version tags (`v*`) trigger the `.github/workflows/docs.yml` workflow, which builds and deploys to Netlify automatically.

**Manual deployment**: Use the "Run workflow" button in GitHub Actions. Requires `NETLIFY_AUTH_TOKEN` and `NETLIFY_SITE_ID` secrets to be configured in the repository settings.

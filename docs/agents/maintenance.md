# Maintenance

## Dependabot alerts

Dependabot alerts from `documentation/pnpm-lock.yaml` affect only the getqui.com build. Runtime-scope `web/` alerts and `go.sum` alerts always need a fix in qui. A development-scope `web/` alert needs a fix only when the code of the package goes into the bundle, for example the polyfills from `vite-plugin-node-polyfills` and the CSS that `web/src/index.css` imports.

## Release and Docker files

The Docker, compose, and release files are at the repo root. Releases build with `.goreleaser.release.yml`, and local builds use `.goreleaser.yml`. Edit both. Before you change the release signing key or the signing steps in `.github/workflows/release.yml`, read ADR 0036.

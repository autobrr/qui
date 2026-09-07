# Repository cleanup audit for #2626

Recorded before implementation at commit `67753dc51173c663420c0e48bda8ec990b39a7c1`.
Source references below use that commit's line numbers.
The scope is [#2626](https://github.com/autobrr/qui/issues/2626), findings 6, 37, and 39 from [#2576](https://github.com/autobrr/qui/issues/2576).

## Claim validation

| Finding | Verdict | Evidence and retained behavior |
| --- | --- | --- |
| 6 | Confirmed | `.github/workflows/{pr-review,claude,docs}.yml.disabled` are inactive archives. No active workflow, script, or manual command invokes them. GitHub's workflow inventory excludes them. `documentation/README.md:31-33` promises the absent docs workflow and its Run workflow button. Keep `documentation/netlify.toml:1-6`, which defines the build command, output directory, and Node version. Keep the active triage and license workflows. |
| 37 | Confirmed | `documentation/package.json:30` declares ReDoc. Searches found it only in the manifest and lockfile, with no source, MDX, script, test, or configuration consumer. Keep `documentation/docs/api/overview.md:11`, which directs readers to qui's Swagger UI at `/api/docs`. Remove ReDoc with pnpm and retain the other dependency versions. |
| 39 | Confirmed | No source, documentation, script, test, import, or asset configuration uses the eight files listed below. `documentation/docusaurus.config.ts:44,110,119` selects `favicon.png`, `qui-hero.png`, and `qui.png`. `documentation/src/pages/index.tsx:53` also uses `qui-hero.png`. Keep these files and the other qui artwork. |

Finding 39 covers these files:

- `web/src/assets/react.svg`
- `documentation/static/img/docusaurus-social-card.jpg`
- `documentation/static/img/docusaurus.png`
- `documentation/static/img/favicon.ico`
- `documentation/static/img/logo.svg`
- `documentation/static/img/undraw_docusaurus_mountain.svg`
- `documentation/static/img/undraw_docusaurus_react.svg`
- `documentation/static/img/undraw_docusaurus_tree.svg`

Repository searches included hidden workflows and manual package commands.
Docusaurus copies static files into its build, so this cleanup removes their unused public URLs.
The explicit favicon configuration remains `img/favicon.png`.
No executable branch or guard is removed. The archived workflow triggers receive no input from GitHub Actions.
No finding is partly resolved, already resolved, or rejected.

## Verification requirements

Run `make precommit`, `make build`, the documentation typecheck, and `make docs-build`.
The release workflow excludes documentation changes at `.github/workflows/release.yml:17,26` and does not build the documentation site.
Inspect the built site's branding, favicon, and API documentation, and open `/api/docs` in an isolated qui instance.
Use CI for the existing unit suites. This cleanup adds no logic that needs a new test.
Keep the seven-day package release age and blocked install scripts when pnpm updates the lockfile.

## Verification results

`make precommit` passed with 0 Go issues and 59 existing frontend warnings.
`make build` produced the frontend bundle and a runnable qui binary.
`make docs-build` generated the static site and processed 43 documents.
The docs build warned about the absent blog directory and Node's experimental localStorage support.
It reported no broken links or missing assets.
pnpm removed 682 lockfile lines without adding entries or changing retained versions.
The removal used `pnpm_config_minimum_release_age=10080` and `pnpm_config_ignore_scripts=true`.

The documentation typecheck failed with TS5102 because TypeScript 7 removed `baseUrl`.
Both the unchanged `documentation/tsconfig.json` and the installed Docusaurus preset set that option.
This failure predates the cleanup. Unit suites remain deferred to CI under the global repository rules.

Served the built docs with `pnpm serve --host 127.0.0.1 --port 32626 --no-open`.
Browser inspection showed the qui logo and hero image, and both images loaded.
The favicon remained `/img/favicon.png` and returned HTTP 200 with `image/png`.
The API page rendered its Swagger UI guidance at `/api/docs`.
No browser errors appeared, and neither build contained references to the removed assets.

Started `./qui serve` with a generated temporary configuration, empty data, and a localhost-only port.
The browser rendered Swagger UI with its endpoint list at `/api/docs`.
`/api/openapi.json` returned HTTP 200 with `application/json`.
The application root returned HTTP 200 with `text/html`.
No browser errors appeared in Swagger UI.
Both temporary server process groups exited, and both localhost ports closed.
Removed the temporary configuration, data, logs, and PID ledger after the inspection.

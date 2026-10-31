# Commits and pull requests

## Before each commit

- Review the diff for over-engineering. If the ponytail plugin (<https://github.com/DietrichGebert/ponytail>) is installed, use its `ponytail:ponytail-review` skill. If it is not, do a trim pass: remove speculative config, unused states, single-caller layers, and duplicate helpers.
  - When a published spec or format defines the logic, keep the well-proven library. It outranks the no-new-dependency rule of ponytail. See `docs/standards/simplicity.md`.
- Conventional commits: `feat(scope):`, `fix(scope):`, etc.

## Review feedback

Before you post or apply a review suggestion that changes or removes a code branch, show in a test or a trace the input that the branch guards. A simplification that reads cleaner can still drop a case that the old guard handled.

## Documentation

- Before you open a PR or add commits to one, review the complete PR diff for documentation needs. If the diff needs Docusaurus documentation, update `documentation/docs/` in the same PR. State in the final report whether you updated the documentation or why no update was needed.
  - Until the SSH setup screen (#2917) ships, a documentation change that describes only remote instances goes in #3330, not in the PR. Add it there as an item with the PR number.
- When available, use the `simple-english`, `unslop`, and `stop-slop` skills for documentation prose.
- Keep `README.md` concise; put feature deep-dives in `documentation/docs/`.
- Netlify builds getqui.com from `main`, so a docs change merged to `develop` goes live with the next release, or earlier through a PR to `main` that cherry-picks the squashed commit (`main` is protected). After that PR merges, a maintainer merges `main` into `develop`.
- Internal notes live in `docs/`. Git ignores `docs/` except the paths that `.gitignore` allows. To commit a new note, add a `!docs/<file>` line to `.gitignore`.

## Title and body

- Fill `.github/pull_request_template.md` into the PR body; `gh pr create --body` does not auto-fill it.
- The PR title becomes the squash commit subject, and GitHub adds " (#N)". Keep the title plus " (#N)" at 72 characters or less. Before you say that any PR is ready to merge, a contributor's PR too, check its title and offer a shorter one if it is too long.

## What CI runs

CI runs `go test ./...` in `release.yml` on pull requests, and with `-race` on pushes to `main`, `develop`, and tags. A change that touches only paths in the `paths-ignore` list of `release.yml` skips it. `lint.yml` runs `go vet ./...` on Windows for every PR. It also runs the native Windows `go test -count=1 ./...` when the PR changes `cmd/`, `internal/`, `pkg/`, `go.mod`, `go.sum`, `.gitattributes`, or `.github/workflows/lint.yml`. `test.yml` runs the Postgres tests, the migration parity check, the automations docs JSON examples test, and the generated JSON reference test, also on a PR that changes only `documentation/`.

## Screenshots

To show screenshots or recordings in a pull request:

1. If brrbin is on `PATH`, run `brrbin publish --help` and follow it. If it exits with code 4, stop and ask the user to run `brrbin login`. If it exits with code 6, do step 3.
2. If brrbin is not installed, run `gh api "orgs/autobrr/teams/brrbin/memberships/$(gh api user --jq .login)" --jq .state`. If it prints `active`, set `GOPRIVATE=github.com/autobrr/brrbin` in the environment and run `go install github.com/autobrr/brrbin@latest`, then do step 1. If Go is not installed, tell the user that brrbin needs Go, and do step 3. If the install cannot read the repo, run `gh auth setup-git` and try again. If brrbin is still not on `PATH`, tell the user to add the folder that `go env GOBIN` prints. If it prints nothing, the folder is `bin` in the first entry of `go env GOPATH`.
3. Otherwise, save the screenshots in a folder and tell the user: "Sign in with GitHub at https://bin.getqui.com, create a Paste with the pull request URL as the issue link, and add the screenshots from `<folder>`. Only qui maintainers can read a Paste." Put the Paste link in the Screenshots section of the pull request. A Paste holds only images, so attach recordings to the pull request. If `gh api "orgs/autobrr/members/$(gh api user --jq .login)" --silent` succeeds, also tell the user: "You can publish private screenshots with brrbin if you are in the autobrr/brrbin team. Ask an org admin to add you."

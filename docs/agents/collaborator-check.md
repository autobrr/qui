# Collaborator check

The section "Who can open issues and pull requests" in `.github/CONTRIBUTING.md` says who can open them on GitHub. Before you plan an issue or a pull request for your user, do this check one time per session:

1. If your user's own agent instructions say that they are a qui collaborator, skip the check.
2. If you run in the triage workflow (`.github/workflows/triage.yml`), skip the check.
3. Otherwise, run `gh api repos/autobrr/qui --jq .permissions.push`.
4. If the output is not `true`, follow that section of `.github/CONTRIBUTING.md`. When it sends your user to Discussions, offer a Discussion draft instead of the issue or the pull request: the Issue Triage form for a bug, or Feature Requests, Ideas for a feature.

Your user can still change the code for their own build.

import json
import os
import subprocess
import sys

SOURCES = "internal/api/testdata/api-key-template-sources.json"
MARKER = "<!-- qui-api-key-template-sources:{} -->"


def fetch_sha(repo, ref, path):
    result = subprocess.run(
        ["gh", "api", f"repos/{repo}/contents/{path}?ref={ref}", "--jq", ".sha"],
        capture_output=True,
        text=True,
    )
    if result.returncode == 0:
        return result.stdout.strip()
    if "HTTP 404" in result.stderr:
        return None
    raise RuntimeError(f"{repo} {path}: {result.stderr.strip()}")


def changed_files(tool, fetch):
    changed = []
    for path, pinned in tool["files"].items():
        current = fetch(tool["repo"], tool["ref"], path)
        if current != pinned:
            changed.append((path, pinned, current))
    return changed


def issue_title(tool):
    return f"Check the API key templates for {tool['name']} again"


def issue_body(tool, changed):
    templates = ", ".join(f"`{t}`" for t in tool["templates"])
    rows = "\n".join(
        f"| [{path}](https://github.com/{tool['repo']}/commits/{tool['ref']}/{path}) | `{pinned[:7]}` | "
        + (f"`{current[:7]}`" if current else "deleted")
        + " |"
        for path, pinned, current in changed
    )
    return f"""{MARKER.format(tool['name'])}
{tool['name']} changed a file that decides which qui routes it calls. The API key templates {templates} can now give it too little or too much access.

| File | Pinned | Now |
| --- | --- | --- |
{rows}

1. Read the qui calls in the changed files.
2. Update the routes of each template in `internal/api/server_api_key_templates_test.go`, and its permissions in `internal/models/api_key_template.go`. Then run `make generate`, which writes the template list of the Settings page, and `go test -count=1 -run TestAPIKeyTemplates ./internal/api`.
3. Update the pins in `{SOURCES}`, and close this issue with that pull request.
"""


def open_issue_for(tool, issues):
    marker = MARKER.format(tool["name"])
    return next((issue for issue in issues if marker in (issue.get("body") or "")), None)


def action(tool, changed, issues):
    if not changed:
        return None
    body = issue_body(tool, changed)
    existing = open_issue_for(tool, issues)
    if existing is None:
        return ("create", issue_title(tool), body)
    if existing["body"].strip() != body.strip():
        return ("edit", existing["number"], body)
    return None


def gh(*args, body=None):
    subprocess.run(["gh", *args], input=body, text=True, check=True)


def main():
    repo = os.environ["GITHUB_REPOSITORY"]
    with open(SOURCES, encoding="utf-8") as f:
        sources = json.load(f)
    issues = json.loads(
        subprocess.run(
            ["gh", "issue", "list", "--repo", repo, "--state", "open", "--limit", "500", "--json", "number,body"],
            capture_output=True,
            text=True,
            check=True,
        ).stdout
    )
    for tool in sources["tools"]:
        changed = changed_files(tool, fetch_sha)
        step = action(tool, changed, issues)
        if step is None:
            print(f"{tool['name']}: no change to report")
        elif step[0] == "create":
            gh("issue", "create", "--repo", repo, "--title", step[1], "--body-file", "-", body=step[2])
        else:
            gh("issue", "edit", str(step[1]), "--repo", repo, "--body-file", "-", body=step[2])
    return 0


if __name__ == "__main__":
    sys.exit(main())

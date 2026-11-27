import json
import os
import subprocess
import sys
from pathlib import Path

LABELS = ("ready-for-human", "needs-grilling")

QUERY = """
query($owner: String!, $name: String!, $number: Int!) {
  repository(owner: $owner, name: $name) {
    pullRequest(number: $number) {
      closingIssuesReferences(first: 100) {
        nodes {
          number
          repository { nameWithOwner }
          labels(first: 100) { nodes { name } }
        }
      }
    }
  }
}
"""


def blocked_issues(response, repository):
    references = response["data"]["repository"]["pullRequest"]["closingIssuesReferences"]["nodes"]
    blocked = []
    for issue in references:
        if issue["repository"]["nameWithOwner"] != repository:
            continue
        names = {label["name"] for label in issue["labels"]["nodes"]}
        label = next((label for label in LABELS if label in names), None)
        if label:
            blocked.append((issue["number"], label))
    return sorted(blocked)


def failures(blocked):
    return [
        f"This PR closes #{number}, which is labelled {label}. A maintainer must decide on that issue first. "
        f"This check passes on a rerun once a maintainer labels the issue ready-for-agent."
        for number, label in blocked
    ]


def closing_references(repository, number):
    owner, name = repository.split("/")
    output = subprocess.run(
        [
            "gh", "api", "graphql",
            "-f", f"query={QUERY}",
            "-F", f"owner={owner}",
            "-F", f"name={name}",
            "-F", f"number={number}",
        ],
        check=True,
        stdout=subprocess.PIPE,
        text=True,
    ).stdout
    return json.loads(output)


if __name__ == "__main__":
    event = json.loads(Path(os.environ["GITHUB_EVENT_PATH"]).read_text())
    repository = os.environ["GITHUB_REPOSITORY"]
    response = closing_references(repository, event["pull_request"]["number"])
    found = failures(blocked_issues(response, repository))
    for error in found:
        print(f"::error::{error}")
    if not found:
        print(f"This PR closes no issue labelled {' or '.join(LABELS)}.")
    sys.exit(bool(found))

import json
import os
import subprocess
import sys
from pathlib import Path

LABEL = "ready-for-human"

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
    return sorted(
        issue["number"]
        for issue in references
        if issue["repository"]["nameWithOwner"] == repository
        and any(label["name"] == LABEL for label in issue["labels"]["nodes"])
    )


def failures(numbers):
    return [
        f"This PR closes #{number}, which is labelled {LABEL}. A maintainer must decide on that issue first. "
        f"This check passes on a rerun once a maintainer labels the issue ready-for-agent."
        for number in numbers
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
        print(f"This PR closes no issue labelled {LABEL}.")
    sys.exit(bool(found))

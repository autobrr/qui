import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

from check_maintainer_decision import blocked_issues

REPOSITORY = "autobrr/qui"
SCRIPT = Path(__file__).with_name("check_maintainer_decision.py")


def issue(number, *labels, repository=REPOSITORY):
    return {
        "number": number,
        "repository": {"nameWithOwner": repository},
        "labels": {"nodes": [{"name": label} for label in labels]},
    }


def response(*issues):
    return {"data": {"repository": {"pullRequest": {"closingIssuesReferences": {"nodes": list(issues)}}}}}


class TestBlockedIssues(unittest.TestCase):
    def test_closing_references(self):
        cases = [
            ("closes nothing", response(), []),
            ("closes a ready-for-agent issue", response(issue(7, "ready-for-agent", "ci")), []),
            ("closes a ready-for-human issue", response(issue(7, "bug", "ready-for-human")), [(7, "ready-for-human")]),
            ("closes a needs-grilling issue", response(issue(7, "needs-grilling", "feature")), [(7, "needs-grilling")]),
            ("an issue with both labels", response(issue(7, "needs-grilling", "ready-for-human")), [(7, "ready-for-human")]),
            (
                "closes each kind",
                response(
                    issue(9, "ready-for-human"),
                    issue(8, "ready-for-agent"),
                    issue(5, "needs-grilling"),
                    issue(3, "ready-for-human"),
                ),
                [(3, "ready-for-human"), (5, "needs-grilling"), (9, "ready-for-human")],
            ),
            (
                "closes waiting issues of another repo",
                response(
                    issue(7, "ready-for-human", repository="autobrr/autobrr"),
                    issue(8, "needs-grilling", repository="autobrr/autobrr"),
                ),
                [],
            ),
        ]
        for name, data, expected in cases:
            with self.subTest(name):
                self.assertEqual(blocked_issues(data, REPOSITORY), expected)


class TestJob(unittest.TestCase):
    def run_job(self, gh_output, gh_exit=0):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "response.json").write_text(json.dumps(gh_output))
            gh = root / "gh"
            gh.write_text(f'#!/bin/sh\necho "$@" > "{root}/args"\ncat "{root}/response.json"\nexit {gh_exit}\n')
            gh.chmod(0o755)
            event = root / "event.json"
            event.write_text(json.dumps({"pull_request": {"number": 42}}))
            result = subprocess.run(
                [sys.executable, "-B", str(SCRIPT)],
                env={
                    **os.environ,
                    "PATH": f"{root}{os.pathsep}{os.environ['PATH']}",
                    "GITHUB_EVENT_PATH": str(event),
                    "GITHUB_REPOSITORY": REPOSITORY,
                },
                capture_output=True,
                text=True,
            )
            args = (root / "args").read_text()
            return result, args

    def test_fails_on_a_ready_for_human_issue(self):
        result, args = self.run_job(response(issue(4663, "ready-for-human")))
        self.assertEqual(result.returncode, 1)
        self.assertIn("::error::This PR closes #4663, which is labelled ready-for-human.", result.stdout)
        self.assertIn("passes on a rerun once a maintainer labels the issue ready-for-agent", result.stdout)
        self.assertIn("owner=autobrr", args)
        self.assertIn("name=qui", args)
        self.assertIn("number=42", args)

    def test_fails_on_a_needs_grilling_issue(self):
        result, _ = self.run_job(response(issue(4663, "needs-grilling")))
        self.assertEqual(result.returncode, 1)
        self.assertIn("::error::This PR closes #4663, which is labelled needs-grilling.", result.stdout)
        self.assertIn("passes on a rerun once a maintainer labels the issue ready-for-agent", result.stdout)

    def test_passes_on_a_ready_for_agent_issue(self):
        result, _ = self.run_job(response(issue(4663, "ready-for-agent")))
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertNotIn("::error::", result.stdout)

    def test_passes_when_the_pr_closes_nothing(self):
        result, _ = self.run_job(response())
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_fails_when_the_api_call_fails(self):
        result, _ = self.run_job({"message": "Bad credentials"}, gh_exit=1)
        self.assertNotEqual(result.returncode, 0)


if __name__ == "__main__":
    unittest.main()

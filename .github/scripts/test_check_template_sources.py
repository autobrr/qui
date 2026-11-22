import json
import pathlib
import unittest

from check_template_sources import SOURCES, action, changed_files, issue_body

TOOL = {
    "name": "dashbrr",
    "repo": "autobrr/dashbrr",
    "ref": "develop",
    "templates": ["dashbrr"],
    "files": {"a.go": "1111111aaaa", "b.go": "2222222bbbb"},
}


def fetch_from(shas):
    return lambda repo, ref, path: shas[path]


class ChangedFiles(unittest.TestCase):
    def test_lists_a_changed_and_a_deleted_file(self):
        changed = changed_files(TOOL, fetch_from({"a.go": "3333333cccc", "b.go": None}))
        self.assertEqual(changed, [("a.go", "1111111aaaa", "3333333cccc"), ("b.go", "2222222bbbb", None)])

    def test_reports_nothing_when_every_pin_matches(self):
        self.assertEqual(changed_files(TOOL, fetch_from(TOOL["files"])), [])


class Action(unittest.TestCase):
    changed = [("a.go", "1111111aaaa", "3333333cccc")]

    def test_opens_an_issue_when_none_is_open(self):
        step = action(TOOL, self.changed, [{"number": 4, "body": "another issue"}])
        self.assertEqual(step[0], "create")
        self.assertIn("https://github.com/autobrr/dashbrr/commits/develop/a.go", step[2])
        self.assertIn("`3333333`", step[2])

    def test_updates_the_open_issue_when_a_file_changed_again(self):
        old = issue_body(TOOL, [("a.go", "1111111aaaa", "9999999dddd")])
        self.assertEqual(action(TOOL, self.changed, [{"number": 7, "body": old}])[:2], ("edit", 7))

    def test_leaves_the_open_issue_alone_when_nothing_new_changed(self):
        body = issue_body(TOOL, self.changed)
        self.assertIsNone(action(TOOL, self.changed, [{"number": 7, "body": body}]))

    def test_does_nothing_without_a_change(self):
        self.assertIsNone(action(TOOL, [], []))


class Sources(unittest.TestCase):
    def test_each_tool_names_a_repo_a_ref_and_its_files(self):
        root = pathlib.Path(__file__).resolve().parents[2]
        sources = json.loads((root / SOURCES).read_text(encoding="utf-8"))
        for tool in sources["tools"]:
            self.assertEqual(set(tool), {"name", "repo", "ref", "templates", "files"}, tool["name"])
            for sha in tool["files"].values():
                self.assertRegex(sha, r"^[0-9a-f]{40}$")


if __name__ == "__main__":
    unittest.main()

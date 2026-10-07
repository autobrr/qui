import unittest
from pathlib import Path

from check_pr_body import check_body

TEMPLATE = Path(".github/pull_request_template.md").read_text()
BODY = "## Description\nAdd a PR gate.\n## Performance\nCI only.\n## Checklist\n- [ ] Reviewed\n"


class TestCheckPRBody(unittest.TestCase):
    def test_template_sections(self):
        cases = [
            ("complete", BODY, []),
            (
                "missing",
                BODY.replace("## Performance\nCI only.\n", ""),
                ["Missing section: Performance"],
            ),
            (
                "comments only",
                BODY.replace("CI only.", "<!-- Explain performance -->"),
                ["Empty or unchanged template section: Performance"],
            ),
            (
                "untouched template",
                TEMPLATE,
                [
                    "Empty or unchanged template section: Description",
                    "Empty or unchanged template section: Behaviour change",
                    "Empty or unchanged template section: How has this been tested?",
                    "Empty or unchanged template section: Performance",
                    "Empty or unchanged template section: Screenshots (for UI changes)",
                    "Empty or unchanged template section: AI disclosure",
                ],
            ),
            (
                "optional empty",
                BODY + "## Behaviour change\n",
                ["Empty or unchanged template section: Behaviour change"],
            ),
            ("optional filled", BODY + "## Behaviour change\nNone.\n", []),
            (
                "fenced heading",
                BODY.replace(
                    "## Performance\nCI only.",
                    "```markdown\n## Performance\nCI only.\n```",
                ),
                ["Missing section: Performance"],
            ),
            (
                "invalid closing fence",
                BODY.replace(
                    "## Performance\nCI only.",
                    "```markdown\n```example\n## Performance\nCI only.\n```",
                ),
                ["Missing section: Performance"],
            ),
            (
                "blank body",
                "",
                ["Missing section: Description", "Missing section: Performance", "Missing section: Checklist"],
            ),
        ]
        for name, body, expected in cases:
            with self.subTest(name=name):
                self.assertEqual(check_body(body, TEMPLATE), expected)


if __name__ == "__main__":
    unittest.main()

import unittest

from mutation_threads import body, mutants, posted_keys, review, summary

SOURCE = {
    "pkg/a/a.go": [
        "package a",
        "",
        "func inRange(n int) bool {",
        "\treturn n >= 2 && n < 10",
        "}",
        "",
        "func next(n int) int {",
        "\treturn -n + 1",
        "}",
    ],
}


def report(*rows, path="pkg/a/a.go"):
    return {"files": [{"file_name": path, "mutations": [dict(zip(("type", "status", "line", "column"), row)) for row in rows]}]}


def read(source=SOURCE):
    return lambda path: source.get(path, [])


def posted(found, commit="head"):
    return posted_keys(comment["body"] for comment in review(found, set(), commit)["comments"])


class MutantsTest(unittest.TestCase):
    def test_only_an_uncaught_mutant_is_listed(self):
        found = mutants(
            report(
                ("CONDITIONALS_BOUNDARY", "LIVED", 4, 11),
                ("CONDITIONALS_NEGATION", "KILLED", 4, 11),
                ("CONDITIONALS_NEGATION", "NOT COVERED", 4, 21),
                ("ARITHMETIC_BASE", "SKIPPED", 8, 12),
                ("ARITHMETIC_BASE", "TIMED OUT", 8, 12),
            ),
            read(),
        )
        self.assertEqual([(m.path, m.line, m.change) for m in found], [("pkg/a/a.go", 4, "`>=` at column 11 becomes `>`")])

    def test_the_change_names_the_operator_at_the_column(self):
        found = mutants(
            report(
                ("CONDITIONALS_NEGATION", "LIVED", 4, 11),
                ("CONDITIONALS_BOUNDARY", "LIVED", 4, 21),
                ("INVERT_NEGATIVES", "LIVED", 8, 9),
                ("ARITHMETIC_BASE", "LIVED", 8, 12),
            ),
            read(),
        )
        self.assertEqual(
            [m.change for m in found],
            [
                "`>=` at column 11 becomes `<`",
                "`<` at column 21 becomes `<=`",
                "`-` at column 9 becomes `+`",
                "`+` at column 12 becomes `-`",
            ],
        )

    def test_a_mutant_that_the_table_does_not_know_is_still_listed(self):
        found = mutants(
            report(("INVERT_LOGICAL", "LIVED", 4, 16), ("ARITHMETIC_BASE", "LIVED", 4, 9), ("ARITHMETIC_BASE", "LIVED", 40, 1)),
            read(),
        )
        self.assertEqual(
            [m.change for m in found],
            [
                "a `ARITHMETIC_BASE` change at column 9",
                "a `INVERT_LOGICAL` change at column 16",
                "a `ARITHMETIC_BASE` change at column 1",
            ],
        )


class ReviewTest(unittest.TestCase):
    def test_the_mutants_of_one_line_share_one_comment(self):
        found = mutants(
            report(
                ("CONDITIONALS_BOUNDARY", "LIVED", 4, 21),
                ("CONDITIONALS_BOUNDARY", "LIVED", 4, 11),
                ("ARITHMETIC_BASE", "LIVED", 8, 12),
            ),
            read(),
        )
        payload = review(found, set(), "head")
        self.assertEqual((payload["commit_id"], payload["event"]), ("head", "COMMENT"))
        self.assertEqual([(c["path"], c["line"], c["side"]) for c in payload["comments"]], [("pkg/a/a.go", 4, "RIGHT"), ("pkg/a/a.go", 8, "RIGHT")])
        first = payload["comments"][0]["body"]
        self.assertIn("- `>=` at column 11 becomes `>`\n- `<` at column 21 becomes `<=`\n", first)
        self.assertIn("Add a test that fails for each change.", first)
        self.assertNotIn("column 12", first)

    def test_a_posted_mutant_is_not_posted_again(self):
        first = mutants(report(("CONDITIONALS_BOUNDARY", "LIVED", 4, 11)), read())
        both = mutants(report(("CONDITIONALS_BOUNDARY", "LIVED", 4, 11), ("CONDITIONALS_BOUNDARY", "LIVED", 4, 21)), read())
        self.assertIsNone(review(first, posted(first), "head"))
        payload = review(both, posted(first), "head")
        self.assertEqual(len(payload["comments"]), 1)
        self.assertNotIn("column 11", payload["comments"][0]["body"])
        self.assertIn("column 21", payload["comments"][0]["body"])

    def test_a_line_that_moves_or_gets_another_indent_keeps_its_key(self):
        before = mutants(report(("CONDITIONALS_BOUNDARY", "LIVED", 4, 11)), read())
        moved = {"pkg/a/a.go": ["package a", "", "", "func inRange(n int) bool {", "\t\treturn n >= 2 && n < 10", "}"]}
        after = mutants(report(("CONDITIONALS_BOUNDARY", "LIVED", 5, 12)), read(moved))
        self.assertIsNone(review(after, posted(before), "head"))

    def test_a_changed_line_and_another_file_get_a_new_key(self):
        before = mutants(report(("CONDITIONALS_BOUNDARY", "LIVED", 4, 11)), read())
        edited = {"pkg/a/a.go": [*SOURCE["pkg/a/a.go"][:3], "\treturn n >= 3 && n < 10"]}
        other = {"pkg/b/b.go": SOURCE["pkg/a/a.go"]}
        self.assertIsNotNone(review(mutants(report(("CONDITIONALS_BOUNDARY", "LIVED", 4, 11)), read(edited)), posted(before), "head"))
        self.assertIsNotNone(
            review(mutants(report(("CONDITIONALS_BOUNDARY", "LIVED", 4, 11), path="pkg/b/b.go"), read(other)), posted(before), "head")
        )

    def test_two_lines_with_the_same_text_are_two_mutants(self):
        twice = {"pkg/a/a.go": ["\tif n >= 2 {", "\t}", "\tif n >= 2 {", "\t}"]}
        one = mutants(report(("CONDITIONALS_BOUNDARY", "LIVED", 1, 7)), read(twice))
        two = mutants(report(("CONDITIONALS_BOUNDARY", "LIVED", 1, 7), ("CONDITIONALS_BOUNDARY", "LIVED", 3, 7)), read(twice))
        payload = review(two, posted(one), "head")
        self.assertEqual([c["line"] for c in payload["comments"]], [3])

    def test_a_posted_line_does_not_hide_a_later_line_with_the_same_text(self):
        twice = {"pkg/a/a.go": ["\tif n >= 2 {", "\t}", "\tif n >= 2 {", "\t}"]}
        first = mutants(report(("CONDITIONALS_BOUNDARY", "LIVED", 1, 7), ("CONDITIONALS_BOUNDARY", "KILLED", 3, 7)), read(twice))
        second = mutants(report(("CONDITIONALS_BOUNDARY", "KILLED", 1, 7), ("CONDITIONALS_BOUNDARY", "LIVED", 3, 7)), read(twice))
        payload = review(second, posted(first), "head")
        self.assertEqual([c["line"] for c in payload["comments"]], [3])

    def test_only_a_marker_counts_as_posted(self):
        found = mutants(report(("CONDITIONALS_BOUNDARY", "LIVED", 4, 11)), read())
        self.assertEqual(posted_keys(["a reply that names qui-mutation", "<!-- qui-test-growth -->", ""]), set())
        self.assertEqual(posted_keys([body(found)]), {found[0].key})


class SummaryTest(unittest.TestCase):
    def test_the_summary_lists_each_uncaught_mutant(self):
        found = mutants(report(("CONDITIONALS_BOUNDARY", "LIVED", 4, 11), ("ARITHMETIC_BASE", "LIVED", 8, 12)), read())
        text = summary(found)
        self.assertIn("- `pkg/a/a.go:4`: `>=` at column 11 becomes `>`\n- `pkg/a/a.go:8`: `+` at column 12 becomes `-`\n", text)
        self.assertNotIn("was caught", text)
        self.assertEqual(summary([]), "Each mutant on a changed Go line that a test runs was caught.\n")


if __name__ == "__main__":
    unittest.main()

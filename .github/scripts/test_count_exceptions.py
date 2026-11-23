import contextlib
import io
import os
import pathlib
import subprocess
import tempfile
import unittest
from unittest import mock

import count_exceptions
from count_exceptions import ASK, FIX, GO_EXCEPTION, NEW, WEB_EXCEPTION, Comment, Found, Side, compare, generous, report

IDENTITY = {name: "qui" for name in ("GIT_AUTHOR_NAME", "GIT_COMMITTER_NAME")} | {name: "qui@example.invalid" for name in ("GIT_AUTHOR_EMAIL", "GIT_COMMITTER_EMAIL")}
REASON = "//nolint:gosec // the path comes from the config"


def go(*comments, text="", broken=False):
    return Side("a.go", text, tuple(Comment(*comment) for comment in comments), broken)


def web(*comments, text="", broken=False):
    return Side("a.ts", text, tuple(Comment(*comment) for comment in comments), broken)


def texts(found):
    return [(item.path, item.line, item.text) for item in found]


class RulesTest(unittest.TestCase):
    def test_go_directives(self):
        side = go((3, REASON), (4, "// #nosec G401"), (5, "//exhaustive:ignore"), (6, "//gosec:disable G304 -- the path comes from the config"), (7, "/* #nosec G204 */"))
        self.assertEqual([item.line for item in generous(side, GO_EXCEPTION)], [3, 4, 5, 6, 7])

    def test_comments_that_only_name_a_directive(self):
        side = go((1, "//nolintlint is a linter"), (2, "// see the nolint below"), (3, "// the docs name #nosec"))
        self.assertEqual(generous(side, GO_EXCEPTION), [])
        side = web((1, "// the docs name eslint-disable-next-line"), (2, "// eslint-disabled is not a directive"), (3, "/** @param name the text that names @ts-ignore */"))
        self.assertEqual(generous(side, WEB_EXCEPTION), [])

    def test_a_ts_directive_after_the_stars_or_slashes_of_its_comment(self):
        side = web((1, "/** @ts-expect-error the type of the package is wrong */"), (3, "/// @ts-ignore"))
        self.assertEqual(texts(generous(side, WEB_EXCEPTION)), [("a.ts", 1, "/** @ts-expect-error the type of the package is wrong */"), ("a.ts", 3, "/// @ts-ignore")])

    def test_a_ts_directive_counts_when_text_follows_its_name_at_once(self):
        side = web((1, "// @ts-ignoreReason"), (2, "/* @ts-expect-errorReason */"), (3, "// eslint-disabled is not a directive"))
        self.assertEqual([found.line for found in generous(side, WEB_EXCEPTION)], [1, 2])

    def test_a_ts_directive_that_typescript_ignores_counts_too(self):
        side = web((1, "// * @ts-ignore"), (2, "/* @ts-nocheck */"), (3, "/// eslint-disable-next-line no-console -- the CLI prints its result"))
        self.assertEqual([found.line for found in generous(side, WEB_EXCEPTION)], [1, 2])

    def test_generous_counts_each_line_of_a_block_comment(self):
        block = "/* eslint-disable no-alert -- the user confirms the delete\n * eslint-disable no-console -- the CLI prints its result\n  @ts-ignore */"
        side = web((4, block), (9, "/*\neslint-disable-next-line no-console\n*/"))
        self.assertEqual(texts(generous(side, WEB_EXCEPTION)), [
            ("a.ts", 4, "/* eslint-disable no-alert -- the user confirms the delete"),
            ("a.ts", 5, "* eslint-disable no-console -- the CLI prints its result"),
            ("a.ts", 6, "@ts-ignore */"),
            ("a.ts", 10, "eslint-disable-next-line no-console"),
        ])

    def test_an_unchanged_comment_cancels_on_both_sides(self):
        base = go((3, REASON), (9, "/*\nnolint:gosec\n*/"))
        head = go((5, REASON), (12, "/*\nnolint:gosec\n*/"))
        self.assertEqual(compare(base, head, GO_EXCEPTION), ([], []))

    def test_added_and_removed_as_multisets_by_comment(self):
        base = go((3, REASON))
        head = go((3, REASON), (8, REASON), (9, "//nolint:errcheck // best effort"))
        added, removed = compare(base, head, GO_EXCEPTION)
        self.assertEqual(texts(added), [("a.go", 8, REASON), ("a.go", 9, "//nolint:errcheck // best effort")])
        self.assertEqual(removed, [])
        added, removed = compare(head, base, GO_EXCEPTION)
        self.assertEqual(added, [])
        self.assertEqual(texts(removed), [("a.go", 8, REASON), ("a.go", 9, "//nolint:errcheck // best effort")])

    def test_each_found_holds_the_whole_text_of_its_comment(self):
        block = "/* documentation\n// @ts-nocheck\n*/"
        self.assertEqual(compare(web((1, block)), web((1, "// @ts-nocheck")), WEB_EXCEPTION), (
            [Found("a.ts", 1, "// @ts-nocheck", "// @ts-nocheck")],
            [Found("a.ts", 2, "// @ts-nocheck", block)],
        ))

    def test_a_misread_file_counts_each_text_on_the_head_lines_without_a_base_twin(self):
        line = 'console.log("@ts-ignore") // eslint-disable-line no-console -- the CLI prints its result'
        base = web((2, "// @ts-ignore"), text="export const a = 1\n// @ts-ignore\nconst name = '@ts-nocheck'\n")
        head = web((2, line), text=f"export const a = 1\n{line}\nconst name = '@ts-nocheck'\n", broken=True)
        self.assertEqual(compare(base, head, WEB_EXCEPTION), ([Found("a.ts", 2, line), Found("a.ts", 2, line)], []))
        self.assertEqual(compare(base._replace(broken=True), head._replace(broken=False), WEB_EXCEPTION), compare(base, head, WEB_EXCEPTION))


def found(path, line, text):
    return Found(path, line, text, text)


MOVED = found("internal/a/read.go", 2, REASON)
TS = found("web/src/b.ts", 7, "// @ts-expect-error the type of the event is wrong upstream")
ESLINT = found("web/src/b.ts", 21, "// eslint-disable-next-line no-console -- the CLI prints its result")
FRESH = [MOVED, TS, ESLINT]


class ReportTest(unittest.TestCase):
    def test_no_new_exception_passes_without_the_label(self):
        for added, removed in (([], []), ([MOVED], [found("internal/b/b.go", 9, REASON)]), ([], FRESH)):
            self.assertEqual(report(added, removed, approved=False), (["This pull request adds no new lint exception. It needs no approval."], True))

    def test_a_removed_exception_pays_only_for_the_same_comment_text(self):
        removed = [found("internal/a/a.go", 11, REASON), found("web/src/b.ts", 7, "// @ts-ignore"), found("web/src/c.ts", 1, "// @ts-nocheck")]
        lines, passed = report(FRESH, removed, approved=False)
        self.assertFalse(passed)
        self.assertEqual(lines, [
            "This pull request adds 2 new lint exceptions:",
            "  web/src/b.ts:7: // @ts-expect-error the type of the event is wrong upstream",
            "  web/src/b.ts:21: // eslint-disable-next-line no-console -- the CLI prints its result",
            "",
            FIX,
            ASK,
            NEW,
        ])

    def test_one_removed_exception_pays_for_one_added_exception(self):
        lines, passed = report([MOVED, MOVED._replace(line=40)], [MOVED._replace(path="internal/b/b.go")], approved=False)
        self.assertFalse(passed)
        self.assertEqual(lines[:2], ["This pull request adds 1 new lint exception:", f"  internal/a/read.go:40: {REASON}"])

    def test_a_directive_line_does_not_pay_for_a_comment_with_another_text(self):
        active = Found("web/src/b.ts", 1, "// @ts-nocheck", "// @ts-nocheck")
        for comment in ("/* documentation\n// @ts-nocheck\n*/", "// @ts-nocheck after the code"):
            self.assertFalse(report([active], [Found("web/src/a.ts", 2, "// @ts-nocheck", comment)], approved=False)[1], comment)

    def test_an_exception_of_a_misread_file_is_never_paid(self):
        misread = Found("web/src/b.ts", 3, "// @ts-ignore")
        self.assertFalse(report([misread], [found("web/src/a.ts", 3, "// @ts-ignore")], approved=False)[1])

    def test_the_label_approves_the_new_ones(self):
        lines, passed = report(FRESH, [], approved=True)
        self.assertTrue(passed)
        self.assertEqual(lines[0], "This pull request adds 3 new lint exceptions:")
        self.assertEqual(lines[-1], 'A maintainer approved them with the label "exceptions-approved".')
        self.assertNotIn(ASK, lines)

    def test_a_long_line_is_cut(self):
        lines, _ = report([found("a.go", 1, "//nolint:gosec // " + "x" * 500)], [], approved=False)
        self.assertEqual(len(lines[1]), len("  a.go:1: ") + 200)

    def test_only_the_exact_label_approves(self):
        for labels, want in (('["chore", "exceptions-approved"]', 0), ('["chore", "not-exceptions-approved"]', 1), ("[]", 1)):
            with mock.patch.object(count_exceptions, "exceptions", return_value=(FRESH, [])), contextlib.redirect_stdout(io.StringIO()):
                self.assertEqual(count_exceptions.main(["base", "head", "--labels", labels]), want, labels)

    def test_a_change_without_new_exceptions_exits_with_zero(self):
        with mock.patch.object(count_exceptions, "exceptions", return_value=([MOVED], [MOVED])), contextlib.redirect_stdout(io.StringIO()):
            self.assertEqual(count_exceptions.main(["base", "head"]), 0)


class EndToEndTest(unittest.TestCase):
    def run_main(self, before, after):
        def git(*args):
            return subprocess.run(["git", "-c", "commit.gpgSign=false", *args], check=True, capture_output=True, text=True).stdout.strip()

        def commit(files, *parent):
            for path in pathlib.Path().iterdir():
                if path.is_file():
                    path.unlink()
            for path, text in files.items():
                pathlib.Path(path).write_text(text)
            git("add", "-A")
            return git("commit-tree", git("write-tree"), "-m", "x", *parent)

        env = {key: value for key, value in os.environ.items() if not key.startswith("GIT_")} | IDENTITY
        with tempfile.TemporaryDirectory() as root, contextlib.chdir(root), mock.patch.dict(os.environ, env, clear=True):
            git("init", "-q")
            base = commit(before)
            head = commit(after, "-p", base)
            out = io.StringIO()
            with contextlib.redirect_stdout(out):
                code = count_exceptions.main(["--labels", "[]", base, head])
        return code, out.getvalue()

    def test_both_helpers_on_a_real_change(self):
        before = {
            "a.go": "package a\n\nconst fixture = `\n//nolint:gosec // a fixture\n`\n",
            "old.ts": "// eslint-disable-next-line no-console -- the CLI prints its result\nconsole.log(1)\n",
            "moved.go": f"package a\n\nvar data, _ = os.ReadFile(path) {REASON}\n",
            "split.go": f"package a\n\nvar text, _ = os.ReadFile(name) {REASON}\n",
        }
        after = {
            "a.go": f"package a\n\nconst fixture = `\n`\n\nvar text, _ = os.ReadFile(name) {REASON}\n",
            "b.tsx": "const view = <p>Don't</p> // @ts-ignore\nconst help = `\n// eslint-disable-next-line no-console\n`\n",
            "renamed.go": f"package a\n\nvar data, _ = os.ReadFile(path) {REASON}\n",
        }
        code, out = self.run_main(before, after)
        self.assertEqual(code, 1)
        self.assertEqual(out.splitlines()[:3], ["This pull request adds 1 new lint exception:", "  b.tsx:1: // @ts-ignore", ""])

    def test_a_directive_on_an_unchanged_line_that_the_change_makes_active(self):
        before = {"a.ts": "export const help = `\n// @ts-nocheck\n`\n"}
        after = {"a.ts": "export const help = 1\n// @ts-nocheck\n\n"}
        code, out = self.run_main(before, after)
        self.assertEqual(code, 1)
        self.assertIn("  a.ts:2: // @ts-nocheck", out.splitlines())


if __name__ == "__main__":
    unittest.main()

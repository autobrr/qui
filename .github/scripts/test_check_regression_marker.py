import contextlib
import io
import os
import pathlib
import subprocess
import tempfile
import unittest
from unittest import mock

import check_regression_marker
from check_regression_marker import unmarked
from count_test_growth import count

IDENTITY = {
    "GIT_AUTHOR_NAME": "test",
    "GIT_AUTHOR_EMAIL": "test@example.invalid",
    "GIT_COMMITTER_NAME": "test",
    "GIT_COMMITTER_EMAIL": "test@example.invalid",
}

GO_TEST = """diff --git a/internal/a/a_test.go b/internal/a/a_test.go
--- a/internal/a/a_test.go
+++ b/internal/a/a_test.go
@@ -10,0 +11,3 @@
+func TestFixed(t *testing.T) {
+	t.Run("case", func(t *testing.T) {})
+}
"""

GO_MARKED = """diff --git a/internal/a/a_test.go b/internal/a/a_test.go
--- a/internal/a/a_test.go
+++ b/internal/a/a_test.go
@@ -10,0 +11,3 @@
+// regression: #4264
+func TestFixed(t *testing.T) {
+}
"""

WEB_TEST = """diff --git a/web/src/b.test.tsx b/web/src/b.test.tsx
--- a/web/src/b.test.tsx
+++ b/web/src/b.test.tsx
@@ -3,0 +4,2 @@
+  it("shows the button at every width", () => {
+  })
"""

WEB_ROW = """diff --git a/web/src/b.test.tsx b/web/src/b.test.tsx
--- a/web/src/b.test.tsx
+++ b/web/src/b.test.tsx
@@ -3,0 +4 @@
+    { width: 320, visible: true },
"""

WORD_ONLY = """diff --git a/internal/a/a_test.go b/internal/a/a_test.go
--- a/internal/a/a_test.go
+++ b/internal/a/a_test.go
@@ -10,0 +11,2 @@
+// a regression test for #4264
+func TestFixed(t *testing.T) {}
"""

RENAMED = """diff --git a/internal/a/a_test.go b/internal/a/a_test.go
--- a/internal/a/a_test.go
+++ b/internal/a/a_test.go
@@ -11 +11 @@
-func TestStartsWhileDown(t *testing.T) {
+func TestStartsWhileProviderIsDown(t *testing.T) {
diff --git a/web/src/b.test.tsx b/web/src/b.test.tsx
--- a/web/src/b.test.tsx
+++ b/web/src/b.test.tsx
@@ -4 +4 @@
-  it("shows the button", () => {
+  it("shows the button at every width", () => {
"""


class UnmarkedTest(unittest.TestCase):
    def test_fix_without_marker_names_each_file(self):
        self.assertEqual(
            unmarked("fix(web): show it", count(GO_TEST + WEB_TEST), {}),
            ["internal/a/a_test.go", "web/src/b.test.tsx"],
        )

    def test_marker_in_the_file_passes(self):
        self.assertEqual(unmarked("fix(web): show it", count(GO_MARKED + WEB_TEST), {}), ["web/src/b.test.tsx"])

    def test_marker_needs_the_exact_form(self):
        self.assertEqual(unmarked("fix: show it", count(WORD_ONLY), {}), ["internal/a/a_test.go"])

    def test_breaking_fix_counts(self):
        self.assertEqual(unmarked("fix(api)!: drop it", count(GO_TEST), {}), ["internal/a/a_test.go"])

    def test_other_types_pass(self):
        for title in ("feat(web): show it", "refactor: fix the name", "chore(fix): x", "fixup: x"):
            with self.subTest(title=title):
                self.assertEqual(unmarked(title, count(GO_TEST), {"internal/a/a_test.go": 1}), [])

    def test_new_web_row_alone_passes(self):
        self.assertEqual(unmarked("fix(web): show it", count(WEB_ROW), {}), [])

    def test_a_renamed_test_passes(self):
        self.assertEqual(unmarked("fix: x", count(RENAMED), {}), [])

    def test_new_go_row_needs_a_marker(self):
        self.assertEqual(unmarked("fix: x", {}, {"internal/a/a_test.go": 1}), ["internal/a/a_test.go"])
        self.assertEqual(unmarked("fix: x", count(GO_MARKED), {"internal/a/a_test.go": 1}), [])


TABLE = """package a

func TestA(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"a", "a"},%s
	} {
		_ = tc
	}
}
"""


class EndToEndTest(unittest.TestCase):
    def run_main(self, title, before, after):
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
                code = check_regression_marker.main(["--title", title, "--number", "7", base, head])
        return code, out.getvalue()

    def test_a_new_row_without_a_marker_fails(self):
        code, out = self.run_main("fix: x", {"a_test.go": TABLE % ""}, {"a_test.go": TABLE % '\n\t\t{"b", "b"},'})
        self.assertEqual(code, 1)
        self.assertIn("::error file=a_test.go::", out)
        self.assertIn("// regression: #7", out)

    def test_a_new_row_with_a_marker_passes(self):
        code, _ = self.run_main("fix: x", {"a_test.go": TABLE % ""}, {"a_test.go": TABLE % '\n\t\t// regression: #7\n\t\t{"b", "b"},'})
        self.assertEqual(code, 0)

    def test_an_edited_row_in_a_renamed_file_passes(self):
        code, _ = self.run_main("fix: x", {"a_test.go": TABLE % ""}, {"b_test.go": (TABLE % "").replace('"a", "a"', '"a", "A"')})
        self.assertEqual(code, 0)

    def test_a_table_in_a_new_file_needs_a_marker(self):
        code, out = self.run_main("fix: x", {"a_test.go": TABLE % ""}, {"a_test.go": TABLE % "", "b_test.go": "package a\n\nvar cases = []struct{ n int }{{1}}\n"})
        self.assertEqual(code, 1)
        self.assertIn("::error file=b_test.go::", out)

    def test_a_file_name_cannot_start_a_workflow_command(self):
        code, out = self.run_main("fix: x", {}, {"a\n::warning::x,y_test.go": TABLE % ""})
        self.assertEqual(code, 1)
        self.assertIn("::error file=a%0A%3A%3Awarning%3A%3Ax%2Cy_test.go::", out)
        self.assertEqual(len(out.splitlines()), 1)


if __name__ == "__main__":
    unittest.main()

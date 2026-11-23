import contextlib
import io
import os
import pathlib
import subprocess
import tempfile
import unittest
from unittest import mock

import count_exceptions
from count_exceptions import ASK, FIX, Found, exceptions, git_diff, new, report

GO_ADDED = """diff --git a/internal/a/a.go b/internal/a/a.go
index 1111111..2222222 100644
--- a/internal/a/a.go
+++ b/internal/a/a.go
@@ -10,0 +11,6 @@ func run() {
+	//nolint:gosec // the path comes from the config
+	data, _ := os.ReadFile(path)
+	sum := md5.Sum(data) // #nosec G401
+	//exhaustive:ignore
+	switch kind {
+	_ = run() //nolint:errcheck // best effort
"""

GO_NOT_EXCEPTIONS = """diff --git a/scripts/ratchet/main.go b/scripts/ratchet/main.go
index 1111111..2222222 100644
--- a/scripts/ratchet/main.go
+++ b/scripts/ratchet/main.go
@@ -1,0 +2,5 @@
+	list, ok := strings.CutPrefix(text, "nolint:")
+	want := "//nolint:gocognit"
+	linters = append(linters, "nolintlint")
+	//nolintlint is a linter
+	url := "https://example.invalid/#nosec"
+	{"directives", "package p\\n\\n//nolint:gosec // reason\\nvar y = 1 // #nosec G115\\n", 0},
+	raw := `//nolint:gosec`
+	quote := '"' + "//nolint:gosec"
+	run() // see the nolint below
+	escaped := "\\" //nolint:gosec"
+	two := `\\` + `//nolint:gosec`
"""

GO_AFTER_A_STRING = """diff --git a/pkg/a/a_test.go b/pkg/a/a_test.go
index 1111111..2222222 100644
--- a/pkg/a/a_test.go
+++ b/pkg/a/a_test.go
@@ -30,0 +31,3 @@
+		{name: "it's \\"closed\\"", err: errors.New("wsasend: aborted")}, //nolint:err113 // a fixture
+		path := `C:\\` //nolint:gosec // a raw string has no escapes
+	run() /* first */ /* #nosec G204 */
"""

WEB_ADDED = """diff --git a/web/src/a.tsx b/web/src/a.tsx
index 1111111..2222222 100644
--- a/web/src/a.tsx
+++ b/web/src/a.tsx
@@ -4,0 +5,4 @@
+    // eslint-disable-next-line react-hooks/exhaustive-deps -- the ref is stable
+    {/* eslint-disable-next-line qui/no-native-title -- the browser draws it */}
+  const value = input as any // eslint-disable-line @typescript-eslint/no-explicit-any -- the library has no types
+    // @ts-expect-error the backend injects it
@@ -40,0 +45,2 @@
+/* eslint-enable */
+const text = "eslint-disable is a directive"
+  "// eslint-disable-next-line no-console",
+  const template = `// eslint-disable-next-line no-console`
+  const quoted = '// @ts-ignore'
+  // the docs name eslint-disable-next-line
+  // eslint-disabled is not a directive
+  const tick = `\\`// eslint-disable-line`
@@ -60,0 +72,2 @@
+      <p>Don't stop</p> {/* eslint-disable-line qui/no-native-title -- one quote does not open a string */}
+  const pattern = "\\"" // @ts-ignore
+// @ts-nocheck
"""

MOVED = """diff --git a/internal/a/a.go b/internal/a/a.go
index 1111111..2222222 100644
--- a/internal/a/a.go
+++ b/internal/a/a.go
@@ -11,2 +10,0 @@ func run() {
-	//nolint:gosec // the path comes from the config
-	data, _ := os.ReadFile(path)
diff --git a/internal/a/read.go b/internal/a/read.go
new file mode 100644
index 0000000..3333333
--- /dev/null
+++ b/internal/a/read.go
@@ -0,0 +1,3 @@
+package a
+		//nolint:gosec // the path comes from the config
+		data, _ := os.ReadFile(path)
"""

SWAPPED = MOVED + """diff --git a/web/src/b.ts b/web/src/b.ts
index 1111111..2222222 100644
--- a/web/src/b.ts
+++ b/web/src/b.ts
@@ -7 +7 @@
-// @ts-ignore
+// @ts-expect-error the type of the event is wrong upstream
@@ -20,0 +21 @@
+// eslint-disable-next-line no-console -- the CLI prints its result
"""

GO_RAW_STRING = """diff --git a/pkg/a/a_test.go b/pkg/a/a_test.go
index 1111111..2222222 100644
--- a/pkg/a/a_test.go
+++ b/pkg/a/a_test.go
@@ -1,2 +1,7 @@
 package a
 
+const fixture = `
+//nolint:gosec // a fixture
+/* #nosec G304 */`
+
+var data, _ = os.ReadFile(path) //nolint:gosec // the path comes from the config
"""

GO_FIXTURE_LINE_REMOVED = """diff --git a/pkg/a/a_test.go b/pkg/a/a_test.go
index 1111111..2222222 100644
--- a/pkg/a/a_test.go
+++ b/pkg/a/a_test.go
@@ -1,6 +1,6 @@
 package a
 
 const fixture = `
-//nolint:gosec // a fixture
 `
+var data, _ = os.ReadFile(path) //nolint:gosec // the path comes from the config
"""

WEB_MULTILINE = """diff --git a/web/src/d.ts b/web/src/d.ts
index 1111111..2222222 100644
--- a/web/src/d.ts
+++ b/web/src/d.ts
@@ -1,4 +1,10 @@
 /*
-the old text
+eslint-disable no-alert -- the user confirms the delete
 */
+/*
+eslint-disable no-console -- the CLI prints its result
+*/
+const help = `
+// eslint-disable-next-line no-console -- a line of the help text
+`
 console.log(1)
"""

TWO_ON_ONE_LINE = """diff --git a/web/src/c.ts b/web/src/c.ts
index 1111111..2222222 100644
--- a/web/src/c.ts
+++ b/web/src/c.ts
@@ -3 +3 @@
-/* eslint-disable no-console -- the CLI prints its result */
+/* eslint-disable no-console -- the CLI prints its result */ /* eslint-disable no-alert -- the user confirms the delete */
"""

DELETED_FILE = """diff --git a/web/src/old.ts b/web/src/old.ts
deleted file mode 100644
index 3333333..0000000
--- a/web/src/old.ts
+++ /dev/null
@@ -1,2 +0,0 @@
-// eslint-disable-next-line no-console
-console.log(1)
"""

OTHER_FILES = """diff --git a/docs/linting.md b/docs/linting.md
index 1111111..2222222 100644
--- a/docs/linting.md
+++ b/docs/linting.md
@@ -1,0 +2,2 @@
+Use an inline //nolint:gosec // reason
+// eslint-disable-next-line no-console
"""


IDENTITY = {name: "qui" for name in ("GIT_AUTHOR_NAME", "GIT_COMMITTER_NAME")} | {name: "qui@example.invalid" for name in ("GIT_AUTHOR_EMAIL", "GIT_COMMITTER_EMAIL")}


def texts(found):
    return [(item.path, item.line, item.text) for item in found]


class ExceptionsTest(unittest.TestCase):
    def test_go_exceptions_with_their_lines(self):
        added, removed = exceptions(GO_ADDED)
        self.assertEqual(texts(added), [
            ("internal/a/a.go", 11, "//nolint:gosec // the path comes from the config"),
            ("internal/a/a.go", 13, "// #nosec G401"),
            ("internal/a/a.go", 14, "//exhaustive:ignore"),
            ("internal/a/a.go", 16, "//nolint:errcheck // best effort"),
        ])
        self.assertEqual(removed, [])

    def test_go_text_that_only_names_an_exception(self):
        self.assertEqual(exceptions(GO_NOT_EXCEPTIONS), ([], []))

    def test_go_exceptions_after_a_string_or_another_comment(self):
        added, _ = exceptions(GO_AFTER_A_STRING)
        self.assertEqual(texts(added), [
            ("pkg/a/a_test.go", 31, "//nolint:err113 // a fixture"),
            ("pkg/a/a_test.go", 32, "//nolint:gosec // a raw string has no escapes"),
            ("pkg/a/a_test.go", 33, "/* #nosec G204 */"),
        ])

    def test_web_exceptions_with_their_lines(self):
        added, removed = exceptions(WEB_ADDED)
        self.assertEqual([(item.line, item.text.split(" -- ")[0]) for item in added], [
            (5, "// eslint-disable-next-line react-hooks/exhaustive-deps"),
            (6, "/* eslint-disable-next-line qui/no-native-title"),
            (7, "// eslint-disable-line @typescript-eslint/no-explicit-any"),
            (8, "// @ts-expect-error the backend injects it"),
            (72, "/* eslint-disable-line qui/no-native-title"),
            (73, "// @ts-ignore"),
            (74, "// @ts-nocheck"),
        ])
        self.assertEqual(removed, [])

    def test_each_exception_on_a_line_counts(self):
        added, removed = exceptions(TWO_ON_ONE_LINE)
        self.assertEqual(texts(added), [
            ("web/src/c.ts", 3, "/* eslint-disable no-console -- the CLI prints its result */"),
            ("web/src/c.ts", 3, "/* eslint-disable no-alert -- the user confirms the delete */"),
        ])
        self.assertEqual(texts(new(added, removed)), [("web/src/c.ts", 3, "/* eslint-disable no-alert -- the user confirms the delete */")])

    def test_a_string_over_several_lines_holds_no_exception(self):
        added, removed = exceptions(GO_RAW_STRING)
        self.assertEqual(texts(added), [("pkg/a/a_test.go", 7, "//nolint:gosec // the path comes from the config")])
        self.assertEqual(removed, [])

    def test_a_string_that_opens_on_an_unchanged_line(self):
        added, removed = exceptions(GO_FIXTURE_LINE_REMOVED)
        self.assertEqual(texts(added), [("pkg/a/a_test.go", 5, "//nolint:gosec // the path comes from the config")])
        self.assertEqual(removed, [])

    def test_a_directive_on_its_own_line_of_a_block_comment(self):
        added, removed = exceptions(WEB_MULTILINE)
        self.assertEqual(texts(added), [
            ("web/src/d.ts", 2, "eslint-disable no-alert -- the user confirms the delete"),
            ("web/src/d.ts", 5, "eslint-disable no-console -- the CLI prints its result"),
        ])
        self.assertEqual(removed, [])

    def test_removed_exceptions(self):
        added, removed = exceptions(DELETED_FILE)
        self.assertEqual(added, [])
        self.assertEqual(texts(removed), [("web/src/old.ts", 0, "// eslint-disable-next-line no-console")])

    def test_files_that_are_not_code(self):
        self.assertEqual(exceptions(OTHER_FILES), ([], []))

    def test_a_moved_exception_is_not_new(self):
        added, removed = exceptions(MOVED)
        self.assertEqual((len(added), len(removed)), (1, 1))
        self.assertEqual(new(added, removed), [])

    def test_new_names_only_the_exceptions_without_a_removed_twin(self):
        added, removed = exceptions(SWAPPED)
        self.assertEqual(texts(new(added, removed)), [
            ("web/src/b.ts", 7, "// @ts-expect-error the type of the event is wrong upstream"),
            ("web/src/b.ts", 21, "// eslint-disable-next-line no-console -- the CLI prints its result"),
        ])

    def test_two_equal_exceptions_and_one_removed_twin(self):
        same = "//nolint:gosec // the path comes from the config"
        added = [Found("a.go", 1, same), Found("b.go", 2, same)]
        self.assertEqual(new(added, [Found("c.go", 0, same)]), [Found("b.go", 2, same)])


class ReportTest(unittest.TestCase):
    def test_no_more_than_removed_passes_without_the_label(self):
        for diff in ("", MOVED, DELETED_FILE):
            lines, passed = report(*exceptions(diff), approved=False)
            self.assertTrue(passed, diff)
            self.assertEqual(len(lines), 1)
            self.assertIn("It needs no approval.", lines[0])

    def test_more_than_removed_fails_and_lists_the_new_ones(self):
        lines, passed = report(*exceptions(SWAPPED), approved=False)
        self.assertFalse(passed)
        self.assertEqual(lines, [
            "This pull request adds 3 lint exceptions and removes 2. The new ones:",
            "  web/src/b.ts:7: // @ts-expect-error the type of the event is wrong upstream",
            "  web/src/b.ts:21: // eslint-disable-next-line no-console -- the CLI prints its result",
            "",
            FIX,
            ASK,
        ])

    def test_the_label_approves_the_new_ones(self):
        lines, passed = report(*exceptions(GO_ADDED), approved=True)
        self.assertTrue(passed)
        self.assertEqual(lines[0], "This pull request adds 4 lint exceptions and removes 0. The new ones:")
        self.assertEqual(lines[-1], 'A maintainer approved them with the label "exceptions-approved".')
        self.assertNotIn(ASK, lines)

    def test_one_exception_is_singular(self):
        lines, _ = report([Found("a.go", 1, "//nolint:gosec // reason")], [], approved=False)
        self.assertEqual(lines[0], "This pull request adds 1 lint exception and removes 0. The new ones:")

    def test_only_the_exact_label_approves(self):
        for labels, want in (('["chore", "exceptions-approved"]', 0), ('["chore", "not-exceptions-approved"]', 1), ("[]", 1)):
            with mock.patch.object(count_exceptions, "git_diff", return_value=GO_ADDED), contextlib.redirect_stdout(io.StringIO()):
                self.assertEqual(count_exceptions.main(["base", "head", "--labels", labels]), want, labels)

    def test_a_change_without_new_exceptions_exits_with_zero(self):
        with mock.patch.object(count_exceptions, "git_diff", return_value=MOVED), contextlib.redirect_stdout(io.StringIO()):
            self.assertEqual(count_exceptions.main(["base", "head"]), 0)

    def test_a_long_line_is_cut(self):
        lines, _ = report([Found("a.go", 1, "//nolint:gosec // " + "x" * 500)], [], approved=False)
        self.assertEqual(len(lines[1]), len("  a.go:1: ") + 200)


class GitDiffTest(unittest.TestCase):
    def test_the_diff_holds_the_unchanged_lines(self):
        def git(*args):
            return subprocess.run(["git", "-c", "commit.gpgSign=false", *args], check=True, capture_output=True, text=True).stdout.strip()

        def commit(text, *parent):
            pathlib.Path("a.go").write_text(text)
            git("add", "a.go")
            return git("commit-tree", git("write-tree"), "-m", "x", *parent)

        env = {key: value for key, value in os.environ.items() if not key.startswith("GIT_")} | IDENTITY
        with tempfile.TemporaryDirectory() as root, contextlib.chdir(root), mock.patch.dict(os.environ, env, clear=True):
            git("init", "-q")
            base = commit("package a\n\nconst fixture = `\n//nolint:gosec // a fixture\n`\n")
            head = commit("package a\n\nconst fixture = `\n`\nvar data, _ = os.ReadFile(path) //nolint:gosec // the path comes from the config\n", "-p", base)
            added, removed = exceptions(git_diff(base, head))
        self.assertEqual(texts(added), [("a.go", 5, "//nolint:gosec // the path comes from the config")])
        self.assertEqual(removed, [])


if __name__ == "__main__":
    unittest.main()

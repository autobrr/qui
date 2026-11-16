import unittest

from count_test_growth import Growth, comment, count, is_large, totals


def summary(diff):
    return {name: (g.net_cases, g.added_lines) for name, g in totals(count(diff)).items()}


GO_ADDED = """diff --git a/internal/a/a_test.go b/internal/a/a_test.go
index 1111111..2222222 100644
--- a/internal/a/a_test.go
+++ b/internal/a/a_test.go
@@ -10,0 +11,9 @@ func TestOld(t *testing.T) {
+func TestNew(t *testing.T) {
+	for _, tc := range []struct{ name string }{
+		{name: "first"},
+		{name: "second"},
+	} {
+		t.Run(tc.name, func(t *testing.T) {})
+	}
+}
+func BenchmarkNew(b *testing.B) { b.Run("x", func(b *testing.B) {}) }
"""

GO_REMOVED = """diff --git a/internal/a/a_test.go b/internal/a/a_test.go
index 1111111..2222222 100644
--- a/internal/a/a_test.go
+++ b/internal/a/a_test.go
@@ -3,4 +2,0 @@
-func TestGone(t *testing.T) {
-	t.Run("one", func(t *testing.T) {})
-	t.Run("two", func(t *testing.T) {})
-}
@@ -20 +18 @@
-func TestRenamed(t *testing.T) {
+func TestRenamedAgain(t *testing.T) {
"""

DELETED_FILE = """diff --git a/web/src/old.test.ts b/web/src/old.test.ts
deleted file mode 100644
index 3333333..0000000
--- a/web/src/old.test.ts
+++ /dev/null
@@ -1,4 +0,0 @@
-describe("old", () => {
-  it("works", () => {})
-  test.skip("later", () => {})
-})
"""

RENAMED_FILE = """diff --git a/web/src/a.test.tsx b/web/src/b.test.tsx
similarity index 100%
rename from web/src/a.test.tsx
rename to web/src/b.test.tsx
"""

RENAMED_AND_EDITED = """diff --git a/web/src/a.test.tsx b/web/src/b.test.tsx
similarity index 90%
rename from web/src/a.test.tsx
rename to web/src/b.test.tsx
index 4444444..5555555 100644
--- a/web/src/a.test.tsx
+++ b/web/src/b.test.tsx
@@ -5,0 +6 @@
+  it("renders", () => {})
"""

WEB_MODIFIERS = """diff --git a/web/src/c.test.ts b/web/src/c.test.ts
new file mode 100644
index 0000000..6666666
--- /dev/null
+++ b/web/src/c.test.ts
@@ -0,0 +1,10 @@
+import { expect, it, test } from "vitest"
+it.each([1, 2])("case %i", (n) => {
+  expect(/x/.test("x")).toBe(true)
+  const edit = (x: number) => x
+})
+test.each`
+  a | b
+`("table $a", () => {})
+test.only.skipIf(ci)("flaky", () => {})
+test.extend({})
"""

GO_TABLE_ROW = """diff --git a/internal/a/a_test.go b/internal/a/a_test.go
index 1111111..2222222 100644
--- a/internal/a/a_test.go
+++ b/internal/a/a_test.go
@@ -12,0 +13,2 @@ func TestTable(t *testing.T) {
+		{name: "second", want: 2},
+		{name: "third", want: 3},
"""

NOT_A_TEST_FILE = """diff --git a/internal/a/a.go b/internal/a/a.go
index 1111111..2222222 100644
--- a/internal/a/a.go
+++ b/internal/a/a.go
@@ -1,0 +2 @@
+func TestHelper(t *testing.T) {}
"""

TYPED_EACH = """diff --git a/web/src/d.test.ts b/web/src/d.test.ts
index 7777777..8888888 100644
--- a/web/src/d.test.ts
+++ b/web/src/d.test.ts
@@ -3,0 +4,2 @@
+  it.each<[string, number]>([
+  it.each<[Parameters<typeof view>[0], number]>([
"""

SPECIAL_PATHS = """diff --git a/web/a b.test.ts b/web/a b.test.ts
new file mode 100644
index 0000000..1111111
--- /dev/null
+++ b/web/a b.test.ts\t
@@ -0,0 +1 @@
+it("space", () => {})
diff --git "a/web/q\\"x.test.ts" "b/web/q\\"x.test.ts"
new file mode 100644
index 0000000..2222222
--- /dev/null
+++ "b/web/q\\"x.test.ts"
@@ -0,0 +1 @@
+it("quote", () => {})
diff --git "a/web/\\303\\251.test.ts" "b/web/\\303\\251.test.ts"
deleted file mode 100644
index 3333333..0000000
--- "a/web/\\303\\251.test.ts"
+++ /dev/null
@@ -1 +0,0 @@
-it("accent", () => {})
diff --git "a/web/caf\\351.test.ts" "b/web/caf\\351.test.ts"
new file mode 100644
index 0000000..4444444
--- /dev/null
+++ "b/web/caf\\351.test.ts"
@@ -0,0 +1 @@
+it("latin-1", () => {})
"""

REMOVED_DASHES = """diff --git a/internal/a/a_test.go b/internal/a/a_test.go
index 1111111..2222222 100644
--- a/internal/a/a_test.go
+++ b/internal/a/a_test.go
@@ -4 +4 @@
--- a/web/src/x.test.ts
+++ b/web/src/x.test.ts
"""


class TestCount(unittest.TestCase):
    def test_diffs(self):
        cases = [
            ("go added cases", GO_ADDED, {"go": (4, 9), "web": (0, 0)}),
            ("go removed and renamed cases", GO_REMOVED, {"go": (-3, 1), "web": (0, 0)}),
            ("deleted file", DELETED_FILE, {"go": (0, 0), "web": (-2, 0)}),
            ("renamed file", RENAMED_FILE, {"go": (0, 0), "web": (0, 0)}),
            ("renamed and edited file", RENAMED_AND_EDITED, {"go": (0, 0), "web": (1, 1)}),
            ("web modifiers", WEB_MODIFIERS, {"go": (0, 0), "web": (3, 10)}),
            ("table row is not a case", GO_TABLE_ROW, {"go": (0, 2), "web": (0, 0)}),
            ("not a test file", NOT_A_TEST_FILE, {"go": (0, 0), "web": (0, 0)}),
            ("typed each", TYPED_EACH, {"go": (0, 0), "web": (2, 2)}),
            ("hunk lines that look like headers", REMOVED_DASHES, {"go": (0, 1), "web": (0, 0)}),
            ("all at once", GO_ADDED + DELETED_FILE + WEB_MODIFIERS, {"go": (4, 9), "web": (1, 10)}),
        ]
        for name, diff, want in cases:
            with self.subTest(name):
                self.assertEqual(summary(diff), want)


class TestFiles(unittest.TestCase):
    def test_counts_for_each_file(self):
        files = count(GO_ADDED + DELETED_FILE + RENAMED_FILE + RENAMED_AND_EDITED + NOT_A_TEST_FILE)
        self.assertEqual(
            files,
            {
                "internal/a/a_test.go": Growth(added_cases=4, added_lines=9),
                "web/src/old.test.ts": Growth(removed_cases=2),
                "web/src/b.test.tsx": Growth(added_cases=1, added_lines=1),
            },
        )

    def test_paths_that_git_quotes(self):
        self.assertEqual(
            count(SPECIAL_PATHS),
            {
                "web/a b.test.ts": Growth(added_cases=1, added_lines=1),
                'web/q"x.test.ts': Growth(added_cases=1, added_lines=1),
                "web/\u00e9.test.ts": Growth(removed_cases=1),
                "web/caf\ufffd.test.ts": Growth(added_cases=1, added_lines=1),
            },
        )


def grown(cases, lines):
    return Growth(added_cases=cases, added_lines=lines)


class TestThreshold(unittest.TestCase):
    def test_is_large(self):
        cases = [
            ("below both", {"a_test.go": grown(19, 599)}, False),
            ("cases at threshold", {"a_test.go": grown(20, 0)}, True),
            ("lines at threshold", {"a_test.go": grown(0, 600)}, True),
            ("sum over files", {"a_test.go": grown(10, 300), "b.test.ts": grown(10, 300)}, True),
            ("removed cases count", {"a_test.go": grown(25, 0), "b.test.ts": Growth(removed_cases=6)}, False),
        ]
        for name, files, want in cases:
            with self.subTest(name):
                self.assertEqual(is_large(files), want)


def table(body):
    return [line for line in body.splitlines() if line.startswith("| ") and not line.startswith("| File ")]


class TestComment(unittest.TestCase):
    def test_layout(self):
        body = comment(count(GO_ADDED + DELETED_FILE + WEB_MODIFIERS))
        self.assertEqual(
            body,
            """<!-- qui-test-growth -->
**This pull request adds 5 test cases and 19 lines of tests.** It does not block the merge.

<details>
<summary>Where the tests are (3 files)</summary>

| File | Test cases | Lines |
|---|---:|---:|
| `internal/a/a_test.go` | +4 | 9 |
| `web/src/c.test.ts` | +3 | 10 |
| `web/src/old.test.ts` | -2 | 0 |
| **Go** | **+4** | **9** |
| **Web** | **+1** | **10** |

</details>

<details>
<summary>Why this comment appears</summary>

A test case is a Go test or benchmark function, a subtest, or a web `it` or `test` call. This comment appears when a pull request adds 20 or more test cases, or 600 or more lines to test files.

</details>

<details>
<summary>Prompt for an AI agent</summary>

```text
Read docs/standards/tests.md. For each test and each table row that this
pull request adds, name the code path that no other test runs. Fold or
delete each one that has no such code path. Do not add tests.
```

</details>

<sub>This comment updates on each push. Read the <a href="https://github.com/autobrr/qui/blob/develop/docs/standards/tests.md">test standards</a>.</sub>
""",
        )

    def test_rows(self):
        def files(n):
            return {f"f{i}_test.go": grown(i, 10) for i in range(1, n + 1)}

        cases = [
            (
                "five files, no more row",
                files(5),
                ["f5_test.go", "f4_test.go", "f3_test.go", "f2_test.go", "f1_test.go", "**Go**"],
            ),
            (
                "one more file gets its own row",
                files(6),
                ["f6_test.go", "f5_test.go", "f4_test.go", "f3_test.go", "f2_test.go", "f1_test.go", "**Go**"],
            ),
            (
                "two more files fold into one row",
                files(7),
                ["f7_test.go", "f6_test.go", "f5_test.go", "f4_test.go", "f3_test.go", "2 more files | +3 | 20", "**Go**"],
            ),
            (
                "lines break a tie",
                {"a_test.go": grown(2, 5), "b_test.go": grown(2, 50), "c_test.go": grown(3, 1)},
                ["c_test.go", "b_test.go", "a_test.go", "**Go**"],
            ),
            (
                "files that remove as many or more cases than they add",
                {
                    "a.test.ts": Growth(added_cases=1, removed_cases=4, added_lines=3),
                    "b.test.ts": grown(1, 1),
                    "c.test.ts": Growth(added_cases=1, removed_cases=1, added_lines=2),
                },
                ["b.test.ts` | +1 | 1", "c.test.ts` | 0 | 2", "a.test.ts` | -3 | 3", "**Web** | **-2** | **6**"],
            ),
            (
                "a pipe in a path",
                {"web/a|b.test.ts": grown(1, 1)},
                ["`web/a\\|b.test.ts` | +1 | 1", "**Web**"],
            ),
        ]
        for name, given, want in cases:
            with self.subTest(name):
                rows = table(comment(given))
                self.assertEqual(len(rows), len(want), rows)
                for row, part in zip(rows, want):
                    self.assertIn(part, row)

    def test_headline(self):
        cases = [
            ("commas", {"a_test.go": grown(1234, 5678)}, "adds 1,234 test cases and 5,678 lines of tests.", "(1 file)"),
            ("singular", {"a_test.go": grown(1, 1)}, "adds 1 test case and 1 line of tests.", "(1 file)"),
            (
                "net removal",
                {"a_test.go": Growth(added_cases=2, removed_cases=7, added_lines=700), "b.test.ts": grown(0, 1)},
                "removes 5 test cases and adds 701 lines of tests.",
                "(2 files)",
            ),
        ]
        for name, files, headline, summary in cases:
            with self.subTest(name):
                body = comment(files)
                self.assertIn(f"**This pull request {headline}**", body)
                self.assertIn(f"<summary>Where the tests are {summary}</summary>", body)


if __name__ == "__main__":
    unittest.main()

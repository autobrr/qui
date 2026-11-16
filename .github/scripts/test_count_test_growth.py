import unittest

from count_test_growth import comment, count, is_large


def summary(diff):
    return {name: (g.net_cases, g.added_lines) for name, g in count(diff).items()}


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
            ("hunk lines that look like headers", REMOVED_DASHES, {"go": (0, 1), "web": (0, 0)}),
            ("all at once", GO_ADDED + DELETED_FILE + WEB_MODIFIERS, {"go": (4, 9), "web": (1, 10)}),
        ]
        for name, diff, want in cases:
            with self.subTest(name):
                self.assertEqual(summary(diff), want)


def growth_with(cases, lines):
    growth = count("")
    growth["go"].added_cases = cases
    growth["go"].added_lines = lines
    return growth


class TestThreshold(unittest.TestCase):
    def test_is_large(self):
        cases = [
            ("below both", 19, 599, False),
            ("cases at threshold", 20, 0, True),
            ("lines at threshold", 0, 600, True),
        ]
        for name, cases_added, lines_added, want in cases:
            with self.subTest(name):
                self.assertEqual(is_large(growth_with(cases_added, lines_added)), want)

    def test_net_cases_use_removed_cases(self):
        growth = growth_with(25, 0)
        growth["web"].removed_cases = 6
        self.assertFalse(is_large(growth))


class TestComment(unittest.TestCase):
    def test_numbers(self):
        growth = count(GO_ADDED + DELETED_FILE + WEB_MODIFIERS)
        body = comment(growth)
        self.assertTrue(body.startswith("<!-- qui-test-growth -->\n"))
        self.assertIn("| Test cases, added minus removed | +4 | +1 | +5 |", body)
        self.assertIn("| Lines added to test files | 9 | 10 | 19 |", body)
        self.assertIn("docs/standards/tests.md", body)


if __name__ == "__main__":
    unittest.main()

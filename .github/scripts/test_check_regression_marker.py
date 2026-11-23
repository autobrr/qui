import unittest

from check_regression_marker import unmarked
from count_test_growth import count

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


class UnmarkedTest(unittest.TestCase):
    def test_fix_without_marker_names_each_file(self):
        self.assertEqual(
            unmarked("fix(web): show it", count(GO_TEST + WEB_TEST)),
            ["internal/a/a_test.go", "web/src/b.test.tsx"],
        )

    def test_marker_in_the_file_passes(self):
        self.assertEqual(unmarked("fix(web): show it", count(GO_MARKED + WEB_TEST)), ["web/src/b.test.tsx"])

    def test_marker_needs_the_exact_form(self):
        self.assertEqual(unmarked("fix: show it", count(WORD_ONLY)), ["internal/a/a_test.go"])

    def test_breaking_fix_counts(self):
        self.assertEqual(unmarked("fix(api)!: drop it", count(GO_TEST)), ["internal/a/a_test.go"])

    def test_other_types_pass(self):
        for title in ("feat(web): show it", "refactor: fix the name", "chore(fix): x", "fixup: x"):
            with self.subTest(title=title):
                self.assertEqual(unmarked(title, count(GO_TEST)), [])

    def test_new_row_alone_passes(self):
        self.assertEqual(unmarked("fix(web): show it", count(WEB_ROW)), [])


if __name__ == "__main__":
    unittest.main()

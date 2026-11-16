import argparse
import re
import subprocess
import sys
from dataclasses import dataclass

CASE_THRESHOLD = 20
LINE_THRESHOLD = 600
MARKER = "<!-- qui-test-growth -->"
STANDARDS_URL = "https://github.com/autobrr/qui/blob/develop/docs/standards/tests.md"
PATHSPECS = ["*_test.go", "*.test.ts", "*.test.tsx"]

GO_CASE = re.compile(r"\bfunc (?:Test|Benchmark)\w*\(|\b[tb]\.Run\(")
WEB_CASE = re.compile(
    r"(?<![\w.$])(?:it|test)"
    r"(?:\.(?:each|only|skip|todo|concurrent|sequential|fails|for|skipIf|runIf))*"
    r"\s*[(`]"
)


@dataclass
class Growth:
    added_cases: int = 0
    removed_cases: int = 0
    added_lines: int = 0

    @property
    def net_cases(self):
        return self.added_cases - self.removed_cases


def kind(path):
    if path.endswith("_test.go"):
        return "go"
    if path.endswith((".test.ts", ".test.tsx")):
        return "web"
    return None


def count(diff):
    growth = {"go": Growth(), "web": Growth()}
    current = None
    in_hunk = False
    for line in diff.splitlines():
        if line.startswith("diff --git "):
            current = None
            in_hunk = False
        elif not in_hunk and line.startswith(("--- a/", "+++ b/")):
            current = kind(line[6:])
        elif line.startswith("@@"):
            in_hunk = True
        elif in_hunk and current and line[:1] in ("+", "-"):
            pattern = GO_CASE if current == "go" else WEB_CASE
            cases = len(pattern.findall(line[1:]))
            if line[0] == "+":
                growth[current].added_cases += cases
                growth[current].added_lines += 1
            else:
                growth[current].removed_cases += cases
    return growth


def is_large(growth):
    net = sum(g.net_cases for g in growth.values())
    lines = sum(g.added_lines for g in growth.values())
    return net >= CASE_THRESHOLD or lines >= LINE_THRESHOLD


def signed(n):
    return f"+{n}" if n > 0 else str(n)


def comment(growth):
    go, web = growth["go"], growth["web"]
    return "\n".join([
        MARKER,
        "This pull request adds many tests.",
        "",
        "| | Go | Web | Total |",
        "|---|---:|---:|---:|",
        f"| Test cases, added minus removed | {signed(go.net_cases)} | {signed(web.net_cases)} | {signed(go.net_cases + web.net_cases)} |",
        f"| Lines added to test files | {go.added_lines} | {web.added_lines} | {go.added_lines + web.added_lines} |",
        "",
        "A test case is a Go test or benchmark function, a subtest, or a web `it` or `test` call. "
        f"This comment appears when a pull request adds {CASE_THRESHOLD} or more test cases, or {LINE_THRESHOLD} or more lines to test files. "
        "It does not block the merge. "
        f"Make sure that each new test obeys the [test standards]({STANDARDS_URL}). "
        "For example, a new table row must run a branch that no other row runs.",
        "",
    ])


def git_diff(base, head):
    return subprocess.run(
        ["git", "diff", "--no-color", "--no-ext-diff", "--src-prefix=a/", "--dst-prefix=b/", "-U0", "-M", f"{base}...{head}", "--", *PATHSPECS],
        check=True,
        capture_output=True,
        text=True,
    ).stdout


def main(argv=None):
    parser = argparse.ArgumentParser(description="Count the test cases and test lines that a change adds.")
    parser.add_argument("base")
    parser.add_argument("head")
    parser.add_argument(
        "--comment",
        action="store_true",
        help="print the pull request comment, or nothing when the change is below both thresholds",
    )
    args = parser.parse_args(argv)
    growth = count(git_diff(args.base, args.head))
    if args.comment:
        if is_large(growth):
            sys.stdout.write(comment(growth))
        return
    for name, g in growth.items():
        print(f"{name}: cases {signed(g.net_cases)} (+{g.added_cases} -{g.removed_cases}), lines +{g.added_lines}")
    print(f"comment: {'yes' if is_large(growth) else 'no'}")


if __name__ == "__main__":
    main()

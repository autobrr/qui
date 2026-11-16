import argparse
import ast
import re
import subprocess
import sys
from dataclasses import dataclass

CASE_THRESHOLD = 20
LINE_THRESHOLD = 600
MARKER = "<!-- qui-test-growth -->"
STANDARDS_URL = "https://github.com/autobrr/qui/blob/develop/docs/standards/tests.md"
PATHSPECS = ["*_test.go", "*.test.ts", "*.test.tsx"]
TOP_FILES = 5
PROMPT = """Read docs/standards/tests.md. For each test and each table row that this
pull request adds, name the code path that no other test runs. Fold or
delete each one that has no such code path. Do not add tests."""

GO_CASE = re.compile(r"\bfunc (?:Test|Benchmark)\w*\(|\b[tb]\.Run\(")
WEB_CASE = re.compile(
    r"(?<![\w.$])(?:it|test)"
    r"(?:\.(?:each|only|skip|todo|concurrent|sequential|fails|for|skipIf|runIf))*"
    r"(?:<.*?>)?\s*[(`]"
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


def header_path(line):
    name = line[4:].rstrip("\t")
    if name.startswith('"'):
        name = ast.literal_eval(name).encode("latin-1").decode("utf-8", "replace")
    return name[2:] if name.startswith(("a/", "b/")) else None


def count(diff):
    files = {}
    path = None
    in_hunk = False
    for line in diff.splitlines():
        if line.startswith("diff --git "):
            path = None
            in_hunk = False
        elif not in_hunk and line.startswith(("--- ", "+++ ")):
            name = header_path(line)
            if name is not None:
                path = name if kind(name) else None
        elif line.startswith("@@"):
            in_hunk = True
        elif in_hunk and path and line[:1] in ("+", "-"):
            pattern = GO_CASE if kind(path) == "go" else WEB_CASE
            cases = len(pattern.findall(line[1:]))
            growth = files.setdefault(path, Growth())
            if line[0] == "+":
                growth.added_cases += cases
                growth.added_lines += 1
            else:
                growth.removed_cases += cases
    return files


def total(growths):
    result = Growth()
    for growth in growths:
        result.added_cases += growth.added_cases
        result.removed_cases += growth.removed_cases
        result.added_lines += growth.added_lines
    return result


def totals(files):
    return {side: total(g for path, g in files.items() if kind(path) == side) for side in ("go", "web")}


def is_large(files):
    growth = total(files.values())
    return growth.net_cases >= CASE_THRESHOLD or growth.added_lines >= LINE_THRESHOLD


def signed(n):
    return f"+{n:,}" if n > 0 else f"{n:,}"


def plural(n, word):
    return f"{n:,} {word}" if n == 1 else f"{n:,} {word}s"


def row(label, growth, bold=False):
    cells = [label, signed(growth.net_cases), f"{growth.added_lines:,}"]
    if bold:
        cells = [f"**{cell}**" for cell in cells]
    return "| " + " | ".join(cells) + " |"


def comment(files):
    growth = total(files.values())
    if growth.net_cases >= 0:
        cases = f"adds {plural(growth.net_cases, 'test case')} and"
    else:
        cases = f"removes {plural(-growth.net_cases, 'test case')} and adds"
    ranked = sorted(files.items(), key=lambda item: (-item[1].net_cases, -item[1].added_lines, item[0]))
    shown = ranked if len(ranked) <= TOP_FILES + 1 else ranked[:TOP_FILES]
    rows = [row("`" + path.replace("|", "\\|") + "`", g) for path, g in shown]
    rest = ranked[len(shown):]
    if rest:
        rows.append(row(f"{len(rest):,} more files", total(g for _, g in rest)))
    sides = totals(files)
    rows += [
        row(name, sides[side], bold=True)
        for side, name in (("go", "Go"), ("web", "Web"))
        if any(kind(path) == side for path in files)
    ]
    return "\n".join([
        MARKER,
        f"**This pull request {cases} {plural(growth.added_lines, 'line')} of tests.** "
        "It does not block the merge.",
        "",
        "<details>",
        f"<summary>Where the tests are ({plural(len(files), 'file')})</summary>",
        "",
        "| File | Test cases | Lines |",
        "|---|---:|---:|",
        *rows,
        "",
        "</details>",
        "",
        "<details>",
        "<summary>Why this comment appears</summary>",
        "",
        "A test case is a Go test or benchmark function, a subtest, or a web `it` or `test` call. "
        f"This comment appears when a pull request adds {CASE_THRESHOLD} or more test cases, "
        f"or {LINE_THRESHOLD} or more lines to test files.",
        "",
        "</details>",
        "",
        "<details>",
        "<summary>Prompt for an AI agent</summary>",
        "",
        "```text",
        PROMPT,
        "```",
        "",
        "</details>",
        "",
        f'<sub>This comment updates on each push. Read the <a href="{STANDARDS_URL}">test standards</a>.</sub>',
        "",
    ])


def git_diff(base, head):
    return subprocess.run(
        ["git", "-c", "core.quotePath=true", "diff", "--no-color", "--no-ext-diff", "--src-prefix=a/", "--dst-prefix=b/", "-U0", "-M", f"{base}...{head}", "--", *PATHSPECS],
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
    files = count(git_diff(args.base, args.head))
    if args.comment:
        if is_large(files):
            sys.stdout.write(comment(files))
        return
    for name, g in totals(files).items():
        print(f"{name}: cases {signed(g.net_cases)} (+{g.added_cases} -{g.removed_cases}), lines +{g.added_lines}")
    print(f"comment: {'yes' if is_large(files) else 'no'}")


if __name__ == "__main__":
    main()

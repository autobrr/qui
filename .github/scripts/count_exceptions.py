import argparse
import json
import re
import subprocess
import sys
from collections import Counter
from typing import NamedTuple

from count_test_growth import header_path

LABEL = "exceptions-approved"
WEB_SUFFIXES = (".ts", ".tsx", ".mts", ".cts", ".js", ".jsx", ".mjs", ".cjs")
PATHSPECS = ["*.go", *(f"*{suffix}" for suffix in WEB_SUFFIXES)]
GO_EXCEPTION = re.compile(r"\s*(?:nolint|#nosec|exhaustive:ignore)\b")
WEB_EXCEPTION = re.compile(r"\s*(?:eslint-disable|@ts-(?:expect-error|ignore|nocheck))\b")
QUOTES = "\"'`"
HUNK = re.compile(r"^@@ -\d+(?:,\d+)? \+(\d+)")
SHOWN = 200
FIX = (
    "An exception hides a report of a check. Fix each report instead: "
    "change the code until the check passes, and delete the exception."
)
ASK = (
    "If the code is right on purpose, keep the exception with its reason on the line, "
    f'and ask a maintainer to set the label "{LABEL}" on the pull request. '
    'Only a maintainer sets that label. See "Exceptions" in docs/linting.md.'
)


class Found(NamedTuple):
    path: str
    line: int
    text: str


def closing(line, start, escapes):
    at = start + 1
    while at < len(line):
        if escapes and line[at] == "\\":
            at += 2
        elif line[at] == line[start]:
            return at
        else:
            at += 1
    return None


def comments(line, raw):
    at = 0
    while at < len(line):
        if line[at] in QUOTES:
            end = closing(line, at, line[at] not in raw)
            at = at + 1 if end is None else end + 1
        elif line.startswith("//", at):
            yield at, line[at + 2:]
            return
        elif line.startswith("/*", at):
            end = line.find("*/", at + 2)
            yield at, line[at + 2:] if end < 0 else line[at + 2:end]
            if end < 0:
                return
            at = end + 2
        else:
            at += 1


def exception(path, line):
    if path.endswith(".go"):
        pattern, raw = GO_EXCEPTION, "`"
    elif path.endswith(WEB_SUFFIXES):
        pattern, raw = WEB_EXCEPTION, ""
    else:
        return None
    for start, text in comments(line, raw):
        if pattern.match(text):
            return line[start:].strip()
    return None


def exceptions(diff):
    added, removed = [], []
    path = None
    in_hunk = False
    number = 0
    for line in diff.splitlines():
        if line.startswith("diff --git "):
            path = None
            in_hunk = False
        elif not in_hunk and line.startswith(("--- ", "+++ ")):
            name = header_path(line)
            if name is not None:
                path = name
        elif line.startswith("@@"):
            in_hunk = True
            start = HUNK.match(line)
            number = int(start.group(1)) if start else 0
        elif in_hunk and path and line[:1] in ("+", "-"):
            text = exception(path, line[1:])
            if text:
                (added if line[0] == "+" else removed).append(Found(path, number, text))
            if line[0] == "+":
                number += 1
    return added, removed


def new(added, removed):
    moved = Counter(found.text for found in removed)
    result = []
    for found in added:
        if moved[found.text] > 0:
            moved[found.text] -= 1
        else:
            result.append(found)
    return result


def plural(n, word):
    return f"{n} {word}" if n == 1 else f"{n} {word}s"


def report(added, removed, approved):
    if len(added) <= len(removed):
        return [f"This pull request adds {plural(len(added), 'lint exception')} and removes {len(removed)}. It needs no approval."], True
    lines = [f"This pull request adds {plural(len(added), 'lint exception')} and removes {len(removed)}. The new ones:"]
    lines += [f"  {found.path}:{found.line}: {found.text[:SHOWN]}" for found in new(added, removed)]
    lines.append("")
    if approved:
        return [*lines, f'A maintainer approved them with the label "{LABEL}".'], True
    return [*lines, FIX, ASK], False


def git_diff(base, head):
    return subprocess.run(
        ["git", "-c", "core.quotePath=true", "diff", "--no-color", "--no-ext-diff", "--src-prefix=a/", "--dst-prefix=b/", "-U0", "-M", f"{base}...{head}", "--", *PATHSPECS],
        check=True,
        capture_output=True,
        text=True,
        errors="replace",
    ).stdout


def main(argv=None):
    parser = argparse.ArgumentParser(description="Fail when a change adds more lint exceptions than it removes, unless a maintainer approved them with a label.")
    parser.add_argument("base")
    parser.add_argument("head")
    parser.add_argument("--labels", default="[]", help="the labels of the pull request, as a JSON list of names")
    args = parser.parse_args(argv)
    added, removed = exceptions(git_diff(args.base, args.head))
    lines, passed = report(added, removed, LABEL in json.loads(args.labels))
    print("\n".join(lines))
    return 0 if passed else 1


if __name__ == "__main__":
    sys.exit(main())

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
GO_EXCEPTION = re.compile(r"\s*(?:nolint|#nosec|gosec:disable|exhaustive:ignore)\b")
WEB_EXCEPTION = re.compile(r"\s*(?:eslint-disable|@ts-(?:expect-error|ignore|nocheck))\b")
QUOTES = "\"'`"
HUNK = re.compile(r"^@@ -\d+(?:,\d+)? \+(\d+)")
SHOWN = 200
WHOLE_FILE = "-U2147483647"
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


def closing(line, at, quote, escapes):
    while at < len(line):
        if escapes and line[at] == "\\":
            at += 2
        elif line[at] == quote:
            return at
        else:
            at += 1
    return None


def comments(line, raw, inside):
    found = []
    at = 0
    while inside or at < len(line):
        if inside == "/*" or not inside and line.startswith("/*", at):
            body = at if inside else at + 2
            end = line.find("*/", body)
            if end < 0:
                found.append((line[at:], line[body:]))
                return found, "/*"
            found.append((line[at:end + 2], line[body:end]))
            at, inside = end + 2, None
        elif inside or line[at] in QUOTES:
            quote = inside or line[at]
            end = closing(line, at if inside else at + 1, quote, quote not in raw)
            if end is None and quote == "`":
                return found, quote
            at, inside = at + 1 if end is None else end + 1, None
        elif line.startswith("//", at):
            found.append((line[at:], line[at + 2:]))
            break
        else:
            at += 1
    return found, None


def exception(path, line, inside):
    if path.endswith(".go"):
        pattern, raw = GO_EXCEPTION, "`"
    elif path.endswith(WEB_SUFFIXES):
        pattern, raw = WEB_EXCEPTION, ""
    else:
        return [], None
    found, inside = comments(line, raw, inside)
    return [text.strip() for text, body in found if pattern.match(body)], inside


def exceptions(diff):
    added, removed = [], []
    path = None
    in_hunk = False
    number = 0
    inside = {}
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
            inside = {}
        elif in_hunk and path and line[:1] in (" ", "+", "-"):
            for side in "+-" if line[0] == " " else line[0]:
                texts, inside[side] = exception(path, line[1:], inside.get(side))
                if side == line[0]:
                    (added if side == "+" else removed).extend(Found(path, number, text) for text in texts)
            if line[0] != "-":
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
        ["git", "-c", "core.quotePath=true", "diff", "--no-color", "--no-ext-diff", "--src-prefix=a/", "--dst-prefix=b/", WHOLE_FILE, "-M", f"{base}...{head}", "--", *PATHSPECS],
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

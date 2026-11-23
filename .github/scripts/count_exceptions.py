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
QUOTES = "\"'"
WORD = re.compile(r"[\w$]+")
REGEX = re.compile(r"/(?:\\.|\[(?:\\.|[^\]\\])*\]|[^/\\\[])+/")
TEMPLATE = re.compile(r"(?:\\.?|\$(?!\{)|[^`\\$])*")
RAW = re.compile(r"[^`]*")
KEYWORDS = {"return", "typeof", "instanceof", "in", "of", "new", "delete", "void", "throw", "case", "do", "else", "yield", "await"}
OPERAND_ENDS = ")]}<"
START = ((), True)
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


def closing(line, at, quote):
    while at < len(line):
        if line[at] == "\\":
            at += 2
        elif line[at] == quote:
            return at
        else:
            at += 1
    return None


def comments(line, state, js):
    stack, regex = state
    found, at, mark = [], 0, 0
    while at < len(line):
        top = stack[-1] if stack else ""
        char = line[at]
        if top == "/*":
            end = line.find("*/", at)
            stop = len(line) if end < 0 else end
            found.append((line[mark:stop + 2], line[at:stop]))
            at, stack = (stop, stack) if end < 0 else (end + 2, stack[:-1])
        elif top == "`":
            at = (TEMPLATE if js else RAW).match(line, at).end()
            if line.startswith("`", at):
                stack, regex, at = stack[:-1], False, at + 1
            elif line.startswith("${", at):
                stack, regex, at = (*stack, "${"), True, at + 2
        elif line.startswith("//", at):
            found.append((line[at:], line[at + 2:]))
            break
        elif line.startswith("/*", at):
            stack, mark, at = (*stack, "/*"), at, at + 2
        elif char == "`":
            stack, at = (*stack, "`"), at + 1
        elif char in QUOTES:
            end = closing(line, at + 1, char)
            at, regex = (at + 1, regex) if end is None else (end + 1, False)
        elif js and regex and (match := REGEX.match(line, at)):
            at, regex = match.end(), False
        elif match := WORD.match(line, at):
            at, regex = match.end(), match.group() in KEYWORDS
        else:
            if char == "{" and stack:
                stack = (*stack, "{")
            elif char == "}" and stack:
                stack = stack[:-1]
            regex = regex if char.isspace() else char not in OPERAND_ENDS
            at += 1
    return found, (stack, regex)


def files(diff):
    path, lines, in_hunk = None, [], False
    for line in diff.splitlines():
        if line.startswith("diff --git "):
            if path:
                yield path, lines
            path, lines, in_hunk = None, [], False
        elif not in_hunk and line.startswith(("--- ", "+++ ")):
            path = header_path(line) or path
        elif line.startswith("@@") or in_hunk and line[:1] in (" ", "+", "-"):
            in_hunk = True
            lines.append(line)
    if path:
        yield path, lines


def scan(path, lines, side, pattern, js):
    found, plain, state, number = [], [], START, 0
    for line in lines:
        if line.startswith("@@"):
            start = HUNK.match(line)
            state, number = START, int(start.group(1)) if start else 0
            continue
        if line[0] in (" ", side):
            texts, state = comments(line[1:], state, js)
            if line[0] == side:
                found += [Found(path, number, text.strip()) for text, body in texts if pattern.match(body)]
                plain += [Found(path, number, line[1:].strip()) for _ in pattern.finditer(line[1:])]
        number += line[0] != "-"
    return plain if state[0] else found


def exceptions(diff):
    added, removed = [], []
    for path, lines in files(diff):
        if path.endswith(".go"):
            pattern, js = GO_EXCEPTION, False
        elif path.endswith(WEB_SUFFIXES):
            pattern, js = WEB_EXCEPTION, True
        else:
            continue
        added += scan(path, lines, "+", pattern, js)
        removed += scan(path, lines, "-", pattern, js)
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

import argparse
import hashlib
import json
import pathlib
import re
import sys
from collections import defaultdict
from typing import NamedTuple

ADR = "docs/adr/0085-ci-posts-each-go-line-with-an-uncaught-mutant-as-a-review-thread.md"
MARKER = re.compile(r"<!-- qui-mutation ([0-9a-f]{16}#\d+) -->")
CHANGES = {
    "ARITHMETIC_BASE": {"+": "-", "-": "+", "*": "/", "/": "*", "%": "*"},
    "CONDITIONALS_BOUNDARY": {">=": ">", ">": ">=", "<=": "<", "<": "<="},
    "CONDITIONALS_NEGATION": {"==": "!=", "!=": "==", ">=": "<", ">": "<=", "<=": ">", "<": ">="},
    "INCREMENT_DECREMENT": {"++": "--", "--": "++"},
    "INVERT_NEGATIVES": {"-": "+"},
}
HEAD = (
    "**No test fails when this line changes.** Mutation testing made each change below, "
    "and the tests of the package still passed:"
)
FIX = (
    "Add a test that fails for each change. If no test can tell a change from the code as it is, "
    "reply with the reason and resolve this thread. `make mutation-go` runs the same check locally. "
    f"See `{ADR}`."
)


class Mutant(NamedTuple):
    path: str
    line: int
    key: str
    change: str


def change(text, column, kind):
    rest = text.encode()[column - 1 :].decode(errors="replace")
    table = CHANGES.get(kind, {})
    for width in (2, 1):
        old = rest[:width]
        if old in table:
            return f"`{old}` at column {column} becomes `{table[old]}`"
    return f"a `{kind}` change at column {column}"


def uncaught(report):
    return sorted(
        (file["file_name"], mutant["line"], mutant["column"], mutant["type"])
        for file in report.get("files") or []
        for mutant in file.get("mutations") or []
        if mutant.get("status") == "LIVED"
    )


def mutants(report, read):
    found = []
    for path, line, column, kind in uncaught(report):
        source = read(path)
        text = source[line - 1] if 0 < line <= len(source) else ""
        indent = len(text) - len(text.lstrip())
        digest = hashlib.sha256("\n".join((path, text.strip(), kind, str(column - indent))).encode()).hexdigest()[:16]
        nth = sum(other.strip() == text.strip() for other in source[:line])
        found.append(Mutant(path, line, f"{digest}#{nth}", change(text, column, kind)))
    return found


def posted_keys(bodies):
    return {key for body in bodies for key in MARKER.findall(body)}


def body(group):
    markers = "\n".join(f"<!-- qui-mutation {mutant.key} -->" for mutant in group)
    rows = "\n".join(f"- {mutant.change}" for mutant in group)
    return f"{markers}\n{HEAD}\n\n{rows}\n\n{FIX}"


def review(found, posted, commit):
    lines = defaultdict(list)
    for mutant in found:
        if mutant.key not in posted:
            lines[(mutant.path, mutant.line)].append(mutant)
    if not lines:
        return None
    return {
        "commit_id": commit,
        "event": "COMMENT",
        "comments": [
            {"path": path, "line": line, "side": "RIGHT", "body": body(group)} for (path, line), group in lines.items()
        ],
    }


def summary(found):
    if not found:
        return "Each mutant on a changed Go line that a test runs was caught.\n"
    rows = "\n".join(f"- `{mutant.path}:{mutant.line}`: {mutant.change}" for mutant in found)
    return f"No test fails for these changes to changed Go lines:\n\n{rows}\n\n{FIX}\n"


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--report", required=True, type=pathlib.Path)
    parser.add_argument("--root", required=True, type=pathlib.Path)
    parser.add_argument("--posted", required=True, type=pathlib.Path)
    parser.add_argument("--commit", required=True)
    parser.add_argument("--review", required=True, type=pathlib.Path)
    args = parser.parse_args()

    root = args.root.resolve()

    def read(path):
        file = (root / path).resolve()
        if not file.is_relative_to(root) or not file.is_file():
            return []
        return file.read_text(errors="replace").splitlines()

    found = mutants(json.loads(args.report.read_text()), read)
    bodies = [json.loads(line) for line in args.posted.read_text().splitlines() if line.strip()]
    payload = review(found, posted_keys(bodies), args.commit)
    if payload:
        args.review.write_text(json.dumps(payload))
    sys.stdout.write(summary(found))


if __name__ == "__main__":
    main()

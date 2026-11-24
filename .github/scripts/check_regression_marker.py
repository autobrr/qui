import argparse
import json
import re
import subprocess
import sys
from pathlib import Path

from count_test_growth import count, git_diff

FIX_TITLE = re.compile(r"^fix(?:\([^)]*\))?!?:")
ROOT = Path(__file__).resolve().parents[2]
ROWS = ["go", "run", "./scripts/testrows"]
NO_ROWS = {"rows": 0, "broken": False}


def git(*args):
    return subprocess.run(["git", *args], stdout=subprocess.PIPE, check=True).stdout.decode(errors="replace")


def go_test_pairs(base, head):
    fields = git("diff", "--name-status", "-z", "-M", base, head, "--", "*_test.go").split("\0")
    pairs, at = {}, 0
    while at < len(fields) - 1:
        status = fields[at]
        if status[:1] in ("R", "C"):
            old, new = fields[at + 1], fields[at + 2]
            at += 3
        else:
            old = new = fields[at + 1]
            at += 2
        if status[:1] != "D":
            pairs[new] = None if status[:1] == "A" else old
    return pairs


def text(commit, path):
    return git("show", f"{commit}:{path}")


def row_counts(sources):
    if not sources:
        return []
    out = subprocess.run(ROWS, cwd=ROOT, input=json.dumps(sources), stdout=subprocess.PIPE, check=True, encoding="utf-8").stdout
    return json.loads(out)


def new_rows(base, head):
    merge_base = git("merge-base", base, head).strip()
    pairs = go_test_pairs(merge_base, head)
    sources = []
    for new, old in pairs.items():
        sources.append({"name": new, "text": text(head, new)})
        if old is not None:
            sources.append({"name": old, "text": text(merge_base, old)})
    counts = iter(row_counts(sources))
    rows = {}
    for path, old in pairs.items():
        after = next(counts)
        before = next(counts) if old is not None else NO_ROWS
        if not before["broken"] and not after["broken"] and after["rows"] > before["rows"]:
            rows[path] = after["rows"] - before["rows"]
    return rows


def unmarked(title, files, rows):
    if not FIX_TITLE.match(title):
        return []
    paths = {path for path, growth in files.items() if growth.net_cases > 0} | set(rows)
    return sorted(path for path in paths if not (path in files and files[path].added_markers))


def annotation_property(value):
    return value.replace("%", "%25").replace("\r", "%0D").replace("\n", "%0A").replace(":", "%3A").replace(",", "%2C")


def main(argv=None):
    parser = argparse.ArgumentParser(
        description="Fail when a fix adds a test case or a table row to a file without a // regression: #N marker in that file."
    )
    parser.add_argument("--title", required=True, help="the pull request title, or the commit subject")
    parser.add_argument("--number", default="N", help="the pull request number, for the message")
    parser.add_argument("base")
    parser.add_argument("head")
    args = parser.parse_args(argv)
    if not FIX_TITLE.match(args.title):
        print("The change is not a fix.")
        return 0
    paths = unmarked(args.title, count(git_diff(args.base, args.head)), new_rows(args.base, args.head))
    for path in paths:
        print(
            f"::error file={annotation_property(path)}::This fix adds a test case or a table row here without a regression marker. "
            f"Put `// regression: #{args.number}` on the line above the test or the table row that fails "
            "without the fix. N is the number of this pull request or of the issue. "
            "Read docs/standards/tests.md."
        )
    if not paths:
        print("Each file with a new test case or table row has a regression marker.")
    return 1 if paths else 0


if __name__ == "__main__":
    sys.exit(main())

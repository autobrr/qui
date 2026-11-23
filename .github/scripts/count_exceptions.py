import argparse
import json
import pathlib
import re
import subprocess
import sys
from collections import Counter
from operator import attrgetter
from typing import NamedTuple

LABEL = "exceptions-approved"
ROOT = pathlib.Path(__file__).resolve().parents[2]
WEB_SUFFIXES = (".ts", ".tsx", ".mts", ".cts", ".js", ".jsx", ".mjs", ".cjs")
PATHSPECS = ["*.go", *(f"*{suffix}" for suffix in WEB_SUFFIXES)]
GO_EXCEPTION = re.compile(r"\s*(?:nolint|#nosec|gosec:disable|exhaustive:ignore)\b")
WEB_EXCEPTION = re.compile(r"\s*(?:eslint-disable\b|[*/\s]*@ts-(?:expect-error|ignore|nocheck))")
LANGUAGES = {
    "go": (GO_EXCEPTION, ["go", "run", "./scripts/exceptions"]),
    "web": (WEB_EXCEPTION, ["node", ".github/scripts/exceptions/comments.mjs"]),
}
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
NEW = (
    "An exception is new when the pull request removes no comment with the same text. "
    "So an exception that moves with its code is not new, and one with a changed text is."
)


class Found(NamedTuple):
    path: str
    line: int
    text: str
    comment: str = ""


class Comment(NamedTuple):
    line: int
    text: str


class Side(NamedTuple):
    path: str
    text: str = ""
    comments: tuple = ()
    broken: bool = False


def language(path):
    if path.endswith(".go"):
        return "go"
    if path.endswith(WEB_SUFFIXES):
        return "web"
    return None


def body(comment):
    if comment.text.startswith("/*"):
        return comment.text[2:-2] if len(comment.text) >= 4 and comment.text.endswith("*/") else comment.text[2:]
    return comment.text[2:]


def generous(side, pattern):
    found = []
    for comment in side.comments:
        shown = comment.text.split("\n")
        for offset, line in enumerate(body(comment).split("\n")):
            if pattern.match(line if offset == 0 else line.lstrip(" \t*")):
                found.append(Found(side.path, comment.line + offset, shown[offset].strip(), comment.text))
    return found


def plain(side, pattern):
    return [Found(side.path, number, line.strip()) for number, line in enumerate(side.text.split("\n"), 1) for _ in pattern.finditer(line)]


def unpaid(added, removed, key):
    paid = Counter(map(key, removed))
    result = []
    for found in added:
        if paid[key(found)] > 0:
            paid[key(found)] -= 1
        else:
            result.append(found)
    return result


def without(side, other):
    twins = Counter(comment.text for comment in other.comments)
    left = []
    for comment in side.comments:
        if twins[comment.text] > 0:
            twins[comment.text] -= 1
        else:
            left.append(comment)
    return side._replace(comments=tuple(left))


def compare(base, head, pattern):
    if base.broken or head.broken:
        return unpaid(plain(head, pattern), plain(base, pattern), attrgetter("text")), []
    return generous(without(head, base), pattern), generous(without(base, head), pattern)


def changes(base, head):
    out = git("diff", "--raw", "--numstat", "-z", "--no-abbrev", "--no-ext-diff", "-M", f"{base}...{head}", "--", *PATHSPECS).decode(errors="replace")
    fields = iter(out.split("\0"))
    for meta in fields:
        if not meta.startswith(":"):
            return
        _, _, old_blob, new_blob, status = meta.split(" ")
        old_blob, new_blob = (blob if blob.strip("0") else "" for blob in (old_blob, new_blob))
        old_path = next(fields)
        new_path = next(fields) if status[0] in "RC" else old_path
        old_side, new_side = (old_path, old_blob), (new_path, new_blob)
        if old_blob and new_blob and language(old_path) == language(new_path):
            yield language(new_path), old_side, new_side
            continue
        if old_blob and language(old_path):
            yield language(old_path), old_side, (old_path, "")
        if new_blob and language(new_path):
            yield language(new_path), (new_path, ""), new_side


def git(*args, stdin=None):
    return subprocess.run(["git", *args], input=stdin, stdout=subprocess.PIPE, check=True).stdout


def blobs(ids):
    wanted = sorted(set(filter(None, ids)))
    out = git("cat-file", "--batch", stdin="".join(f"{blob}\n" for blob in wanted).encode())
    texts, at = dict.fromkeys(["", *wanted], ""), 0
    for blob in wanted:
        end = out.index(b"\n", at)
        header = out[at:end].split()
        at = end + 1
        if len(header) == 3:
            size = int(header[2])
            if header[1] == b"blob":
                texts[blob] = out[at:at + size].decode(errors="replace")
            at += size + 1
    return texts


def listings(command, sources):
    if not sources:
        return []
    out = subprocess.run(command, cwd=ROOT, input=json.dumps(sources), stdout=subprocess.PIPE, check=True, encoding="utf-8").stdout
    return json.loads(out)


def sides(pairs):
    texts = blobs(blob for _, *both in pairs for _, blob in both)
    filled = {}
    for name, (_, command) in LANGUAGES.items():
        flat = [Side(path, texts[blob]) for lang, *both in pairs if lang == name for path, blob in both]
        listed = listings(command, [{"name": side.path, "text": side.text} for side in flat])
        filled[name] = iter(side._replace(comments=tuple(Comment(c["line"], c["text"]) for c in entry["comments"]), broken=entry["broken"]) for side, entry in zip(flat, listed, strict=True))
    return [(name, next(filled[name]), next(filled[name])) for name, *_ in pairs]


def exceptions(base, head):
    added, removed = [], []
    for name, old, head_side in sides(list(changes(base, head))):
        more, fewer = compare(old, head_side, LANGUAGES[name][0])
        added += more
        removed += fewer
    return added, removed


def plural(n, word):
    return f"{n} {word}" if n == 1 else f"{n} {word}s"


def report(added, removed, approved):
    fresh = unpaid(added, removed, attrgetter("comment"))
    if not fresh:
        return ["This pull request adds no new lint exception. It needs no approval."], True
    lines = [f"This pull request adds {plural(len(fresh), 'new lint exception')}:"]
    lines += [f"  {found.path}:{found.line}: {found.text[:SHOWN]}" for found in fresh]
    lines.append("")
    if approved:
        return [*lines, f'A maintainer approved them with the label "{LABEL}".'], True
    return [*lines, FIX, ASK, NEW], False


def main(argv=None):
    parser = argparse.ArgumentParser(description="Fail when a change adds a lint exception and removes no comment with the same text, unless a maintainer approved it with a label.")
    parser.add_argument("base")
    parser.add_argument("head")
    parser.add_argument("--labels", default="[]", help="the labels of the pull request, as a JSON list of names")
    args = parser.parse_args(argv)
    added, removed = exceptions(args.base, args.head)
    lines, passed = report(added, removed, LABEL in json.loads(args.labels))
    print("\n".join(lines))
    return 0 if passed else 1


if __name__ == "__main__":
    sys.exit(main())

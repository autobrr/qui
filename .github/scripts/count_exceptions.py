import argparse
import json
import pathlib
import re
import subprocess
import sys
from collections import Counter
from typing import NamedTuple

LABEL = "exceptions-approved"
ROOT = pathlib.Path(__file__).resolve().parents[2]
WEB_SUFFIXES = (".ts", ".tsx", ".mts", ".cts", ".js", ".jsx", ".mjs", ".cjs")
PATHSPECS = ["*.go", *(f"*{suffix}" for suffix in WEB_SUFFIXES)]
GO_EXCEPTION = re.compile(r"\s*(?:nolint|#nosec|gosec:disable|exhaustive:ignore)\b")
WEB_EXCEPTION = re.compile(r"\s*(?:eslint-disable|[*/\s]*@ts-(?:expect-error|ignore|nocheck))\b")
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


class Found(NamedTuple):
    path: str
    line: int
    text: str


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
                found.append(Found(side.path, comment.line + offset, shown[offset].strip()))
    return found


def strict(side, pattern):
    return [Found(side.path, comment.line, comment.text.split("\n")[0].strip()) for comment in side.comments if pattern.match(body(comment).split("\n")[0])]


def plain(side, pattern):
    return [Found(side.path, number, line.strip()) for number, line in enumerate(side.text.split("\n"), 1) for _ in pattern.finditer(line)]


def new(added, removed):
    moved = Counter(found.text for found in removed)
    result = []
    for found in added:
        if moved[found.text] > 0:
            moved[found.text] -= 1
        else:
            result.append(found)
    return result


def compare(base, head, pattern):
    if base.broken or head.broken:
        return new(plain(head, pattern), plain(base, pattern)), []
    return new(generous(head, pattern), generous(base, pattern)), new(strict(base, pattern), strict(head, pattern))


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
    if len(added) <= len(removed):
        return [f"This pull request adds {plural(len(added), 'lint exception')} and removes {len(removed)}. It needs no approval."], True
    lines = [f"This pull request adds {plural(len(added), 'lint exception')} and removes {len(removed)}. The new ones:"]
    lines += [f"  {found.path}:{found.line}: {found.text[:SHOWN]}" for found in new(added, removed)]
    lines.append("")
    if approved:
        return [*lines, f'A maintainer approved them with the label "{LABEL}".'], True
    return [*lines, FIX, ASK], False


def main(argv=None):
    parser = argparse.ArgumentParser(description="Fail when a change adds more lint exceptions than it removes, unless a maintainer approved them with a label.")
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

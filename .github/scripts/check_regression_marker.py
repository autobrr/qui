import argparse
import re
import sys

from count_test_growth import count, git_diff

FIX_TITLE = re.compile(r"^fix(?:\([^)]*\))?!?:")


def unmarked(title, files):
    if not FIX_TITLE.match(title):
        return []
    return sorted(path for path, growth in files.items() if growth.added_cases and not growth.added_markers)


def main(argv=None):
    parser = argparse.ArgumentParser(
        description="Fail when a fix adds test cases to a file without a // regression: #N marker in that file."
    )
    parser.add_argument("--title", required=True, help="the pull request title, or the commit subject")
    parser.add_argument("--number", default="N", help="the pull request number, for the message")
    parser.add_argument("base")
    parser.add_argument("head")
    args = parser.parse_args(argv)
    paths = unmarked(args.title, count(git_diff(args.base, args.head)))
    for path in paths:
        print(
            f"::error file={path}::This fix adds a test case here without a regression marker. "
            f"Put `// regression: #{args.number}` on the line above the test or the table row that fails "
            "without the fix. N is the number of this pull request or of the issue. "
            "Read docs/standards/tests.md."
        )
    if not paths:
        print("Each file with new test cases has a regression marker, or the change is not a fix.")
    return 1 if paths else 0


if __name__ == "__main__":
    sys.exit(main())

import json
import os
import re
import sys
from pathlib import Path


def sections(body):
    body = re.sub(r"<!--.*?-->", "", body, flags=re.S)
    result = {}
    heading = None
    fence = None
    for line in body.splitlines():
        marker = re.match(r"^\s{0,3}(`{3,}|~{3,})", line)
        if marker:
            token = marker[1]
            if fence is None:
                fence = token
            elif (
                token[0] == fence[0]
                and len(token) >= len(fence)
                and not line[marker.end():].strip()
            ):
                fence = None
            continue
        match = re.match(r"^ {0,3}## (.+?)\s*$", line) if fence is None else None
        if match:
            heading = match[1]
            result.setdefault(heading, [])
        elif heading is not None:
            result[heading].append(line)
    return {heading: "\n".join(lines).strip() for heading, lines in result.items()}


def check_body(body, template):
    expected = sections(template)
    actual = sections(body)
    errors = []
    for heading, placeholder in expected.items():
        required = heading in {"Description", "Performance", "Checklist"}
        if heading not in actual:
            if required:
                errors.append(f"Missing section: {heading}")
        elif not actual[heading] or (
            heading != "Checklist" and actual[heading] == placeholder
        ):
            errors.append(f"Empty or unchanged template section: {heading}")
    return errors


if __name__ == "__main__":
    event = json.loads(Path(os.environ["GITHUB_EVENT_PATH"]).read_text())
    template = Path(".github/pull_request_template.md").read_text()
    errors = check_body(event["pull_request"]["body"] or "", template)
    for error in errors:
        print(f"::error::{error}")
    if not errors:
        print("PR template sections are complete.")
    sys.exit(bool(errors))

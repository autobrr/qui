import { test } from "node:test";
import assert from "node:assert/strict";
import { releaseBody } from "./releaseBody.ts";

const changelog = `## Changelog

### New Features
* feat(a): one
* feat(b): two
### Bug Fixes
* fix(c): three
### Other Changes
* chore(d): four
* chore(e): five
`;

test("Highlights titles, then counts as subtext", () => {
  const md = `### Important
- **Credential encryption.** Take a backup first.

### Highlights
- **Fewer requests.** Long text.
- **Safer orphan scan.** More text.

A closing paragraph.
${changelog}`;
  assert.equal(
    releaseBody(md),
    "**Important:** Credential encryption.\n- Fewer requests.\n- Safer orphan scan.\n-# 2 new features · 1 bug fix · 2 other changes",
  );
});

test("no Highlights gives only the counts", () => {
  assert.equal(releaseBody(changelog), "2 new features · 1 bug fix · 2 other changes");
});

test("a notice without a bold title uses its first sentence", () => {
  const md = `## ⚠️ BREAKING CHANGE
Enter your key again. The old support is gone.
${changelog}`;
  assert.equal(releaseBody(md), "**Breaking:** Enter your key again.\n2 new features · 1 bug fix · 2 other changes");
});

test("a section heading that is not exactly Highlights is ignored", () => {
  assert.equal(releaseBody(`## Release Highlights\n- **Ignored.** Text.\n${changelog}`), "2 new features · 1 bug fix · 2 other changes");
});

test("a post with no changelog gives no body", () => {
  assert.equal(releaseBody("## Initial Release\nqui is here."), "");
});

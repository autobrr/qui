// Compares the writes of one triage run with the expected outcome of a case.
//
// An expectation has these optional keys. A key that is missing is not checked.
//   noWrite: true              the run must write nothing
//   labels: {has, lacks}       label names on the discussion after the run
//   close: "RESOLVED" | false  the close reason, or false for no close
//   issue: false | {state, labels}   no issue, or one issue; state is a name or a list of names
//   comment: false | {marker, matches, notMatches}
//                              no comment, or one comment; marker is an outcome
//                              name or a list; matches are regular expressions
//                              (case-insensitive) that the body must or must not match
// A case can give `anyOf: [expectation, ...]` in place of one expectation.
// Every run may post at most one comment.

const asList = (v) => (Array.isArray(v) ? v : [v]);

export function finalLabels(initial, writes) {
  const labels = new Set(initial);
  for (const w of writes) {
    if (w.op === "add-label") for (const l of w.labels) labels.add(l);
    if (w.op === "remove-label") for (const l of w.labels) labels.delete(l);
  }
  return labels;
}

const markerOf = (body) => /<!--\s*qui-triage:([a-z-]+)\s*-->\s*$/.exec(body)?.[1] ?? null;

function checkOne(expect, initial, writes) {
  const failures = [];
  const labels = finalLabels(initial, writes);
  const comments = writes.filter((w) => w.op === "comment");
  const closes = writes.filter((w) => w.op === "close");
  const issues = writes.filter((w) => w.op === "create-issue");

  if (expect.noWrite && writes.length) failures.push(`expected no write, got ${writes.map((w) => w.op).join(", ")}`);

  for (const l of expect.labels?.has ?? []) if (!labels.has(l)) failures.push(`label ${l} missing at the end`);
  for (const l of expect.labels?.lacks ?? []) if (labels.has(l)) failures.push(`label ${l} present at the end`);

  if (expect.close === false && closes.length) failures.push(`expected no close, got close ${closes.at(-1).reason}`);
  if (typeof expect.close === "string") {
    if (!closes.length) failures.push(`expected close ${expect.close}, got no close`);
    else if (closes.at(-1).reason !== expect.close) failures.push(`expected close ${expect.close}, got close ${closes.at(-1).reason}`);
  }

  if (expect.issue === false && issues.length) failures.push(`expected no issue, got an issue in ${issues[0].state}`);
  if (expect.issue) {
    if (issues.length !== 1) failures.push(`expected one issue, got ${issues.length}`);
    else {
      const states = asList(expect.issue.state ?? []);
      if (states.length && !states.includes(issues[0].state)) failures.push(`expected an issue in ${states.join(" or ")}, got ${issues[0].state}`);
      for (const l of expect.issue.labels ?? []) if (!issues[0].labels.includes(l)) failures.push(`issue lacks label ${l}`);
    }
  }

  if (expect.comment === false && comments.length) failures.push("expected no comment, got one");
  if (expect.comment) {
    if (!comments.length) failures.push("expected a comment, got none");
    else {
      const body = comments[0].body;
      const markers = asList(expect.comment.marker ?? []);
      if (markers.length && !markers.includes(markerOf(body))) failures.push(`expected comment marker ${markers.join(" or ")}, got ${markerOf(body) ?? "none"}`);
      for (const re of expect.comment.matches ?? []) if (!new RegExp(re, "i").test(body)) failures.push(`comment does not match /${re}/`);
      for (const re of expect.comment.notMatches ?? []) if (new RegExp(re, "i").test(body)) failures.push(`comment matches /${re}/`);
    }
  }
  return failures;
}

// check returns {pass, failures}. failures lists every reason, from the
// alternative that came closest when the case gives `anyOf`.
export function check(caseDef, writes) {
  const always = [];
  const comments = writes.filter((w) => w.op === "comment").length;
  if (comments > 1) always.push(`posted ${comments} comments, at most one is allowed`);
  const alternatives = caseDef.expect.anyOf ?? [caseDef.expect];
  const results = alternatives.map((e) => checkOne(e, caseDef.labels, writes));
  const best = results.reduce((a, b) => (b.length < a.length ? b : a));
  const failures = [...always, ...best.map((f) => (alternatives.length > 1 ? `closest alternative: ${f}` : f))];
  return { pass: failures.length === 0, failures };
}

// verdict decides a case from its run results (true = pass). A case fails
// when half or more of its runs fail. It needs a third run when two disagree.
export function verdict(passes) {
  const fails = passes.filter((p) => !p).length;
  if (passes.length === 2 && fails === 1) return "rerun";
  return fails * 2 >= passes.length ? "fail" : "pass";
}

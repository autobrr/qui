// The frozen world of one case: the snapshot from snapshot.mjs, cut back to
// the moment of the case, and a search over it that works like GitHub search
// closely enough for triage.

import { readFileSync } from "node:fs";
import { join } from "node:path";

const ms = (t) => (t ? Date.parse(t) : NaN);

export function loadCorpus(dir) {
  const read = (name) => JSON.parse(readFileSync(join(dir, name), "utf8"));
  return {
    discussions: read("discussions.json"),
    issues: read("issues.json"),
    pulls: read("pulls.json"),
    labels: read("labels.json"),
    releasesQui: read("releases-qui.json"),
    releasesQbittorrent: read("releases-qbittorrent.json"),
  };
}

// cutWorld returns only what existed at `at`, in the state of that moment.
// `discussion` gets `labels` in place of its labels from the snapshot, because
// GitHub keeps no label history for discussions, and `body` in place of its
// body when it is given.
export function cutWorld(corpus, { at, discussion, labels, body }) {
  const cutoff = ms(at);
  if (Number.isNaN(cutoff)) throw new Error(`invalid moment: ${at}`);
  const existed = (t) => ms(t) <= cutoff;
  // snapshot.mjs saves the edits of an edited item, oldest first.
  const asAt = (item) => ({ ...item, body: item.edits?.findLast((e) => existed(e.editedAt))?.body ?? item.body, edits: undefined });

  const repoLabels = corpus.labels.filter((l) => existed(l.createdAt));
  const labelByName = new Map(repoLabels.map((l) => [l.name, l]));
  const keepLabels = (nodes) => nodes.filter((l) => labelByName.has(l.name)).map((l) => labelByName.get(l.name));

  const cutComments = (nodes) =>
    nodes
      .filter((c) => existed(c.createdAt))
      .map((c) => {
        if (!c.replies) return asAt(c);
        const replies = c.replies.nodes.filter((r) => existed(r.createdAt)).map(asAt);
        return { ...asAt(c), replies: { totalCount: replies.length, nodes: replies } };
      });

  // Takes the snapshot nodes, which still hold their edits.
  const lastActivity = (item, comments) => {
    const stamps = (x) => [x.createdAt, ...(x.edits ?? []).map((e) => e.editedAt)];
    const times = [...stamps(item), ...comments.flatMap((c) => [...stamps(c), ...(c.replies?.nodes ?? []).flatMap(stamps)])].filter(existed);
    if (existed(item.closedAt)) times.push(item.closedAt);
    return times.reduce((a, b) => (ms(b) > ms(a) ? b : a));
  };

  const closedState = (item) =>
    existed(item.closedAt)
      ? { closed: true, closedAt: item.closedAt, stateReason: item.stateReason }
      : { closed: false, closedAt: null, stateReason: null };

  const discussions = corpus.discussions
    .filter((d) => existed(d.createdAt))
    .map((d) => {
      const answered = existed(d.answerChosenAt);
      const answer = (c) => ({ ...c, isAnswer: answered && Boolean(c.isAnswer) });
      const comments = cutComments(d.comments.nodes).map((c) =>
        c.replies ? { ...answer(c), replies: { ...c.replies, nodes: c.replies.nodes.map(answer) } } : answer(c),
      );
      let dLabels = keepLabels(d.labels.nodes);
      if (d.number === discussion) {
        const missing = labels.filter((n) => !labelByName.has(n));
        if (missing.length) throw new Error(`labels did not exist at ${at}: ${missing.join(", ")}`);
        dLabels = labels.map((n) => labelByName.get(n));
      }
      return {
        ...asAt(d),
        ...(d.number === discussion && body !== undefined && { body }),
        ...closedState(d),
        answered,
        answerChosenAt: answered ? d.answerChosenAt : null,
        answerChosenBy: answered ? d.answerChosenBy : null,
        updatedAt: lastActivity(d, d.comments.nodes),
        labels: dLabels,
        comments: { totalCount: comments.length, nodes: comments },
      };
    });
  if (!discussions.some((d) => d.number === discussion)) throw new Error(`discussion #${discussion} did not exist at ${at}`);

  const issues = corpus.issues
    .filter((i) => existed(i.createdAt))
    .map((i) => {
      const comments = cutComments(i.comments.nodes);
      const names = new Set();
      for (const e of i.timelineItems.nodes) {
        if (!existed(e.createdAt) || !e.label) continue;
        if (e.__typename === "LabeledEvent") names.add(e.label.name);
        else names.delete(e.label.name);
      }
      const state = closedState(i);
      return {
        ...asAt(i),
        ...state,
        state: state.closed ? "CLOSED" : "OPEN",
        updatedAt: lastActivity(i, i.comments.nodes),
        labels: [...names].filter((n) => labelByName.has(n)).map((n) => labelByName.get(n)),
        comments: { totalCount: comments.length, nodes: comments },
        timelineItems: undefined,
      };
    });

  const pulls = corpus.pulls
    .filter((p) => existed(p.createdAt))
    .map((p) => {
      const merged = existed(p.mergedAt);
      const closed = merged || existed(p.closedAt);
      return {
        ...asAt(p),
        closed,
        closedAt: closed ? p.closedAt : null,
        mergedAt: merged ? p.mergedAt : null,
        state: merged ? "MERGED" : closed ? "CLOSED" : "OPEN",
        updatedAt: lastActivity({ createdAt: p.createdAt, closedAt: closed ? p.closedAt : null, edits: p.edits }, []),
        labels: keepLabels(p.labels.nodes),
      };
    });

  const releases = (list) => list.filter((r) => !r.isDraft && existed(r.publishedAt ?? r.createdAt));

  return {
    at,
    discussion,
    labels: repoLabels,
    discussions,
    issues,
    pulls,
    releases: {
      "autobrr/qui": releases(corpus.releasesQui),
      "qbittorrent/qbittorrent": releases(corpus.releasesQbittorrent),
    },
  };
}

// A light stemmer, so that "move" finds "moving" the way GitHub search does.
function stem(word) {
  let w = word;
  for (const suffix of ["ies", "ing", "ed", "es", "s"]) {
    if (w.endsWith(suffix) && w.length - suffix.length >= 3) {
      w = w.slice(0, -suffix.length);
      break;
    }
  }
  return w.length > 3 && /[ey]$/.test(w) ? w.slice(0, -1) : w;
}

const WORD = /^[a-z0-9]+$/;
const stemCache = new Map();
function stemsOf(text) {
  let stems = stemCache.get(text);
  if (!stems) {
    stems = new Set((text.match(/[a-z0-9]+/g) ?? []).map(stem));
    stemCache.set(text, stems);
  }
  return stems;
}

const QUALIFIERS = new Set(
  "repo is state type label no author category created closed merged updated in path filename extension language sort org user comments reactions interactions linked milestone assignee mentions involves head base draft review status reason archived".split(" "),
);

function tokenize(q) {
  const tokens = [];
  const re = /(-?)(?:([a-zA-Z-]+):(?!:))?(?:"([^"]*)"|(\S+))/g;
  for (const m of q.matchAll(re)) {
    const key = m[2]?.toLowerCase();
    if (key && !QUALIFIERS.has(key)) {
      tokens.push({ neg: m[1] === "-", value: `${m[2]}:${m[3] ?? m[4]}`, quoted: false });
      continue;
    }
    tokens.push({ neg: m[1] === "-", key, value: m[3] ?? m[4], quoted: m[3] !== undefined });
  }
  return tokens;
}

// parseQuery splits a GitHub search query into text terms and qualifiers.
// Text terms form OR groups: "a b OR c" is a AND (b OR c).
export function parseQuery(q) {
  const out = { groups: [], exclude: [], qualifiers: [] };
  let orNext = false;
  for (const t of tokenize(q ?? "")) {
    if (t.key) {
      out.qualifiers.push(t);
      continue;
    }
    if (!t.quoted && t.value === "OR") {
      orNext = true;
      continue;
    }
    if (!t.quoted && t.value === "AND") continue;
    const term = t.value.toLowerCase();
    if (!term) continue;
    if (t.neg) out.exclude.push(term);
    else if (orNext && out.groups.length) out.groups.at(-1).push(term);
    else out.groups.push([term]);
    orNext = false;
  }
  return out;
}

const textHas = (text, term) => (WORD.test(term) ? stemsOf(text).has(stem(term)) : text.includes(term));

// scoreText returns how well an item matches the text terms, or 0 for no match.
export function scoreText(item, parsed, scopes = ["title", "body", "comments"]) {
  const title = scopes.includes("title") ? (item.title ?? "").toLowerCase() : "";
  const body = scopes.includes("body") ? (item.body ?? "").toLowerCase() : "";
  const comments = scopes.includes("comments")
    ? (item.comments?.nodes ?? [])
        .flatMap((c) => [c.body, ...(c.replies?.nodes ?? []).map((r) => r.body)])
        .join("\n")
        .toLowerCase()
    : "";
  const all = `${title}\n${body}\n${comments}`;
  if (parsed.exclude.some((term) => textHas(all, term))) return 0;
  let score = 1;
  for (const group of parsed.groups) {
    const hit = group.filter((term) => textHas(all, term));
    if (!hit.length) return 0;
    score += hit.reduce((s, term) => s + (textHas(title, term) ? 3 : 1), 0);
  }
  return score;
}

const dateCmp = (value) => {
  const m = /^(>=|<=|>|<)?(.+?)(?:\.\.(.+))?$/.exec(value);
  if (m[3]) return (t) => ms(t) >= ms(m[2]) && ms(t) <= ms(m[3]) + 86399999;
  const d = ms(m[2]);
  switch (m[1]) {
    case ">=":
      return (t) => ms(t) >= d;
    case "<=":
      return (t) => ms(t) <= d + 86399999;
    case ">":
      return (t) => ms(t) > d + 86399999;
    case "<":
      return (t) => ms(t) < d;
    default:
      return (t) => ms(t) >= d && ms(t) <= d + 86399999;
  }
};

// qualifierMatch tests one qualifier. kind is "issue", "pr", or "discussion".
// It returns undefined for a qualifier that it does not know, so that the
// caller can ignore it the way GitHub ignores unknown qualifiers.
export function qualifierMatch(item, kind, q, repo) {
  const v = q.value.toLowerCase();
  const labelNames = item.labels.map((l) => l.name.toLowerCase());
  let ok;
  switch (q.key) {
    case "repo":
      ok = v === repo.toLowerCase();
      break;
    case "is":
    case "state":
      if (v === "open") ok = !item.closed;
      else if (v === "closed") ok = item.closed;
      else if (v === "merged") ok = Boolean(item.mergedAt);
      else if (v === "unmerged") ok = kind === "pr" && !item.mergedAt;
      else if (v === "issue") ok = kind === "issue";
      else if (v === "pr" || v === "pull-request") ok = kind === "pr";
      else if (v === "discussion") ok = kind === "discussion";
      else if (v === "answered") ok = Boolean(item.answered);
      else return undefined;
      break;
    case "type":
      ok = v.replace("pull-request", "pr") === kind;
      break;
    case "label":
      ok = v.split(",").some((n) => labelNames.includes(n));
      break;
    case "no":
      if (v !== "label") return undefined;
      ok = labelNames.length === 0;
      break;
    case "author":
      ok = item.author?.login?.toLowerCase() === v.replace(/^app\//, "");
      break;
    case "category":
      ok = kind === "discussion" && (item.category.name.toLowerCase() === v || item.category.slug === v);
      break;
    case "created":
      ok = dateCmp(q.value)(item.createdAt);
      break;
    case "closed":
      ok = Boolean(item.closedAt) && dateCmp(q.value)(item.closedAt);
      break;
    case "merged":
      ok = Boolean(item.mergedAt) && dateCmp(q.value)(item.mergedAt);
      break;
    case "updated":
      ok = dateCmp(q.value)(item.updatedAt);
      break;
    default:
      return undefined;
  }
  return q.neg ? !ok : ok;
}

// search returns the items that match the query, best match first.
// `kinds` limits the result to "issue", "pr", or "discussion".
export function search(world, query, { kinds, repo = "autobrr/qui" }) {
  const parsed = parseQuery(query);
  const inQ = parsed.qualifiers.filter((q) => q.key === "in");
  const scopes = inQ.length ? inQ.flatMap((q) => q.value.toLowerCase().split(",")) : undefined;
  const pools = { issue: world.issues, pr: world.pulls, discussion: world.discussions };
  const typeQ = parsed.qualifiers.find((q) => (q.key === "is" || q.key === "type") && ["issue", "pr", "pull-request", "discussion"].includes(q.value.toLowerCase()) && !q.neg);
  const wanted = kinds.filter((k) => !typeQ || k === typeQ.value.toLowerCase().replace("pull-request", "pr"));
  const hits = [];
  for (const kind of wanted) {
    for (const item of pools[kind]) {
      if (parsed.qualifiers.some((q) => q.key !== "in" && qualifierMatch(item, kind, q, repo) === false)) continue;
      const score = scoreText(item, parsed, scopes);
      if (score) hits.push({ kind, item, score });
    }
  }
  const textSearch = parsed.groups.length > 0;
  hits.sort((a, b) => (textSearch && b.score - a.score) || ms(b.item.createdAt) - ms(a.item.createdAt));
  return hits;
}

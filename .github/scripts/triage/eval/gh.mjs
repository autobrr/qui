#!/usr/bin/env node
// Stand-in for gh in a triage eval run. It answers the reads that the triage
// profile allows from the frozen world in $QUI_EVAL_WORLD, and refuses every
// other call. It never contacts a network host.
import { execFileSync, spawnSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { parseArgs } from "node:util";
import { parseQuery, search } from "./world.mjs";

class GhError extends Error {}
const fail = (msg) => {
  throw new GhError(msg);
};

const REPO = "autobrr/qui";
const QBIT = "qbittorrent/qbittorrent";

function repoOf(value) {
  if (!value) return REPO;
  const r = value.replace(/^https:\/\/github\.com\//, "").replace(/^github\.com\//, "").toLowerCase();
  if (r === REPO || r === QBIT) return r;
  fail(`GraphQL: Could not resolve to a Repository with the name '${value}'. (repository)`);
}

// The snapshot holds the labels, issues and discussions of autobrr/qui only.
function quiOnly(value) {
  const r = repoOf(value);
  if (r !== REPO) fail(`the eval snapshot has no labels, issues or discussions of ${r}`);
}

// --- output shapes, as gh prints them ---

const labelOut = (l) => ({ id: l.id, name: l.name, description: l.description ?? "", color: l.color });
const userOut = (a) => ({ id: a?.id ?? "", is_bot: !a?.id || a.login?.endsWith("[bot]") || a.login === "github-actions", login: a?.login ?? "ghost", name: a?.name ?? "" });
const isoOrNull = (t) => t ?? null;

function closingPulls(world, number) {
  const re = new RegExp(`\\b(close[sd]?|fix(e[sd])?|resolve[sd]?)\\s+(https://github\\.com/autobrr/qui/issues/|#)${number}\\b`, "i");
  return world.pulls
    .filter((p) => p.mergedAt && re.test(p.body ?? ""))
    .map((p) => ({ id: p.id, number: p.number, url: p.url, repository: { id: "", name: "qui", owner: { id: "", login: "autobrr" } } }));
}

function issueJSON(world, item, isPull) {
  const comments = isPull
    ? []
    : item.comments.nodes.map((c) => ({
        id: c.id,
        author: { login: c.author?.login ?? "ghost" },
        authorAssociation: c.authorAssociation,
        body: c.body,
        createdAt: c.createdAt,
        includesCreatedEdit: false,
        isMinimized: false,
        minimizedReason: "",
        reactionGroups: [],
        url: c.url,
        viewerDidAuthor: false,
      }));
  return {
    assignees: [],
    author: userOut(item.author),
    blockedBy: [],
    blocking: [],
    body: item.body,
    closed: item.closed,
    closedAt: isoOrNull(item.closedAt),
    closedByPullRequestsReferences: isPull ? [] : closingPulls(world, item.number),
    comments,
    createdAt: item.createdAt,
    id: item.id,
    isPinned: false,
    issueType: null,
    labels: item.labels.map(labelOut),
    milestone: null,
    number: item.number,
    parent: null,
    projectCards: [],
    projectItems: [],
    reactionGroups: [],
    state: item.state,
    stateReason: item.closed ? (item.stateReason ?? (isPull ? null : "COMPLETED")) : "",
    subIssues: [],
    subIssuesSummary: { total: 0, completed: 0, percentCompleted: 0 },
    title: item.title,
    updatedAt: item.updatedAt,
    url: item.url,
  };
}
const ISSUE_FIELDS = Object.keys(issueJSON({ pulls: [] }, { labels: [], comments: { nodes: [] } }, true));

function searchJSON(item, kind) {
  const out = {
    assignees: [],
    author: { id: item.author?.id ?? "", is_bot: userOut(item.author).is_bot, login: item.author?.login ?? "ghost", type: "User", url: `https://github.com/${item.author?.login ?? "ghost"}` },
    authorAssociation: item.authorAssociation,
    body: item.body,
    closedAt: isoOrNull(item.closedAt),
    commentsCount: item.comments.totalCount,
    createdAt: item.createdAt,
    id: item.id,
    isLocked: item.locked,
    isPullRequest: kind === "pr",
    labels: item.labels.map(labelOut),
    number: item.number,
    repository: { name: "qui", nameWithOwner: REPO },
    state: item.mergedAt ? "merged" : item.closed ? "closed" : "open",
    title: item.title,
    updatedAt: item.updatedAt,
    url: item.url,
  };
  if (kind === "pr") out.isDraft = item.isDraft;
  return out;
}
const SEARCH_ISSUE_FIELDS = Object.keys(searchJSON({ labels: [], comments: {} }, "issue"));
const SEARCH_PR_FIELDS = [...SEARCH_ISSUE_FIELDS, "isDraft"].sort();

const discussionAuthor = (a) => ({ id: a?.id ?? "", login: a?.login ?? "ghost", name: a?.name ?? "" });
const reactionGroups = () =>
  ["THUMBS_UP", "THUMBS_DOWN", "LAUGH", "HOORAY", "CONFUSED", "HEART", "ROCKET", "EYES"].map((content) => ({ content, totalCount: 0 }));

function commentJSON(c, replies) {
  const out = {
    author: discussionAuthor(c.author),
    body: c.body,
    createdAt: c.createdAt,
    id: c.id,
    isAnswer: c.isAnswer ?? false,
    reactionGroups: reactionGroups(),
  };
  if (replies) out.replies = replies;
  out.upvoteCount = c.upvoteCount ?? 0;
  out.url = c.url;
  return out;
}

function discussionJSON(d, comments) {
  return {
    answerChosenAt: isoOrNull(d.answerChosenAt),
    answerChosenBy: d.answerChosenBy ? discussionAuthor(d.answerChosenBy) : null,
    answered: Boolean(d.answered),
    author: discussionAuthor(d.author),
    body: d.body,
    category: d.category,
    closed: d.closed,
    closedAt: isoOrNull(d.closedAt),
    comments,
    createdAt: d.createdAt,
    id: d.id,
    labels: d.labels.map((l) => ({ color: l.color, id: l.id, name: l.name })),
    locked: d.locked,
    number: d.number,
    reactionGroups: reactionGroups(),
    state: d.closed ? "CLOSED" : "OPEN",
    stateReason: d.closed ? d.stateReason : null,
    title: d.title,
    updatedAt: d.updatedAt,
    url: d.url,
  };
}
const DISCUSSION_VIEW_FIELDS = Object.keys(discussionJSON({ labels: [] }, null));
const DISCUSSION_LIST_FIELDS = DISCUSSION_VIEW_FIELDS.filter((f) => !["comments", "reactionGroups", "state"].includes(f));

// --- output ---

function pick(obj, fields) {
  return Object.fromEntries(fields.map((f) => [f, obj[f]]));
}

function parseFields(value, available) {
  if (value === true || value === "") {
    fail(`Specify one or more comma-separated fields for \`--json\`:\n  ${available.join("\n  ")}`);
  }
  const fields = value.split(",").map((f) => f.trim()).filter(Boolean);
  const unknown = fields.find((f) => !available.includes(f));
  if (unknown) fail(`Unknown JSON field: "${unknown}"\nAvailable fields:\n  ${available.join("\n  ")}`);
  return fields;
}

function emit(opts, available, data, plain) {
  if (opts.template) fail("--template is not supported here; use --jq");
  if (opts.json === undefined) {
    if (opts.jq) fail("cannot use `--jq` without specifying `--json`");
    process.stdout.write(plain());
    return;
  }
  const fields = parseFields(opts.json, available);
  const out = Array.isArray(data) ? data.map((d) => pick(d, fields)) : pick(data, fields);
  const text = JSON.stringify(out, null, 2);
  if (!opts.jq) {
    process.stdout.write(`${text}\n`);
    return;
  }
  const r = spawnSync("jq", ["-r", opts.jq], { input: text, encoding: "utf8" });
  if (r.status !== 0) fail(r.stderr.trim() || "jq failed");
  process.stdout.write(r.stdout);
}

const COMMON = {
  repo: { type: "string", short: "R" },
  json: { type: "string" },
  jq: { type: "string", short: "q" },
  template: { type: "string", short: "t" },
  limit: { type: "string", short: "L" },
  web: { type: "boolean", short: "w" },
};

function args(argv, options) {
  try {
    return parseArgs({ args: argv, options: { ...COMMON, ...options }, allowPositionals: true, strict: true });
  } catch (err) {
    const flag = /'([^']+)'/.exec(err.message)?.[1];
    if (err.code === "ERR_PARSE_ARGS_UNKNOWN_OPTION") fail(`unknown flag: ${flag}`);
    fail(err.message);
  }
}

function limitOf(opts, dflt) {
  if (opts.limit === undefined) return dflt;
  const n = Number(opts.limit);
  if (!Number.isInteger(n) || n < 1) fail(`invalid value for --limit: ${opts.limit}`);
  return n;
}

const labelsCell = (item) => item.labels.map((l) => l.name).join(", ");

// --- commands ---

function labelList(world, argv) {
  const { values: o } = args(argv, { search: { type: "string", short: "S" }, sort: { type: "string" }, order: { type: "string" } });
  quiOnly(o.repo);
  let labels = world.labels.map((l) => ({ ...labelOut(l), createdAt: l.createdAt, isDefault: false, updatedAt: l.createdAt, url: `https://github.com/${REPO}/labels/${encodeURIComponent(l.name)}` }));
  if (o.search) labels = labels.filter((l) => `${l.name} ${l.description}`.toLowerCase().includes(o.search.toLowerCase()));
  if (o.sort === "name") labels.sort((a, b) => a.name.localeCompare(b.name));
  if (o.order === "desc") labels.reverse();
  labels = labels.slice(0, limitOf(o, 30));
  emit(o, ["color", "createdAt", "description", "id", "isDefault", "name", "updatedAt", "url"], labels, () =>
    labels.map((l) => `${l.name}\t${l.description}\t#${l.color}\n`).join(""),
  );
}

function releaseList(world, argv) {
  const { values: o } = args(argv, {
    "exclude-drafts": { type: "boolean" },
    "exclude-pre-releases": { type: "boolean" },
    order: { type: "string", short: "O" },
  });
  const repo = repoOf(o.repo);
  let rel = [...world.releases[repo]].sort((a, b) => Date.parse(b.publishedAt) - Date.parse(a.publishedAt));
  const latest = rel.find((r) => !r.isPrerelease)?.tagName;
  if (o["exclude-pre-releases"]) rel = rel.filter((r) => !r.isPrerelease);
  if (o.order === "asc") rel.reverse();
  rel = rel.slice(0, limitOf(o, 30)).map((r) => ({ ...r, isLatest: r.tagName === latest }));
  emit(o, ["createdAt", "isDraft", "isImmutable", "isLatest", "isPrerelease", "name", "publishedAt", "tagName"], rel, () =>
    rel.map((r) => `${r.name}\t${r.isLatest ? "Latest" : r.isPrerelease ? "Pre-release" : ""}\t${r.tagName}\t${r.publishedAt}\n`).join(""),
  );
}

function findIssueOrPull(world, ref) {
  const n = Number(String(ref).replace(/^#/, "").replace(/^https:\/\/github\.com\/autobrr\/qui\/(issues|pull)\//, ""));
  if (!Number.isInteger(n)) fail(`invalid issue format: "${ref}"`);
  const issue = world.issues.find((i) => i.number === n);
  if (issue) return { item: issue, isPull: false };
  const pull = world.pulls.find((p) => p.number === n);
  if (pull) return { item: pull, isPull: true };
  fail(`GraphQL: Could not resolve to an issue or pull request with the number of ${n}. (repository.issue)`);
}

function issueView(world, argv) {
  const { values: o, positionals } = args(argv, { comments: { type: "boolean", short: "c" } });
  quiOnly(o.repo);
  if (positionals.length !== 1) fail("accepts 1 arg(s), received " + positionals.length);
  const { item, isPull } = findIssueOrPull(world, positionals[0]);
  const json = issueJSON(world, item, isPull);
  emit(o, ISSUE_FIELDS, json, () => {
    if (o.comments) {
      return json.comments.map((c) => `author:\t${c.author.login}\nassociation:\t${c.authorAssociation.toLowerCase()}\nedited:\tfalse\nstatus:\tnone\n--\n${c.body}\n--\n`).join("");
    }
    return [
      `title:\t${json.title}`,
      `state:\t${json.state}`,
      `author:\t${json.author.login}`,
      `labels:\t${json.labels.map((l) => l.name).sort().join(", ")}`,
      `comments:\t${item.comments.totalCount}`,
      "assignees:\t",
      "projects:\t",
      "milestone:\t",
      `number:\t${json.number}`,
      "--",
      `${json.body}\n`,
    ].join("\n");
  });
}

function issueList(world, argv) {
  const { values: o } = args(argv, {
    state: { type: "string", short: "s" },
    label: { type: "string", short: "l", multiple: true },
    search: { type: "string", short: "S" },
    author: { type: "string", short: "A" },
    assignee: { type: "string", short: "a" },
    mention: { type: "string" },
    milestone: { type: "string", short: "m" },
    app: { type: "string" },
  });
  quiOnly(o.repo);
  const state = o.state ?? "open";
  if (!["open", "closed", "all"].includes(state)) fail(`invalid argument "${state}" for "-s, --state" flag: valid values are {open|closed|all}`);
  const quals = [];
  if (state !== "all") quals.push(`is:${state}`);
  for (const l of (o.label ?? []).flatMap((x) => x.split(","))) quals.push(`label:"${l}"`);
  if (o.author) quals.push(`author:${o.author}`);
  if (o.app) quals.push(`author:app/${o.app}`);
  const hits = search(world, `${o.search ?? ""} ${quals.join(" ")}`, { kinds: ["issue"] });
  if (!o.search) hits.sort((a, b) => Date.parse(b.item.createdAt) - Date.parse(a.item.createdAt));
  const items = hits.slice(0, limitOf(o, 30)).map((h) => issueJSON(world, h.item, false));
  emit(o, ISSUE_FIELDS, items, () =>
    items.map((i) => `${i.number}\t${i.state}\t${i.title}\t${i.labels.map((l) => l.name).join(", ")}\t${i.updatedAt}\n`).join(""),
  );
}

function discussionList(world, argv) {
  const { values: o } = args(argv, {
    state: { type: "string", short: "s" },
    label: { type: "string", short: "l", multiple: true },
    search: { type: "string", short: "S" },
    author: { type: "string", short: "A" },
    category: { type: "string", short: "c" },
    answered: { type: "boolean" },
    sort: { type: "string" },
    order: { type: "string" },
    after: { type: "string" },
  });
  quiOnly(o.repo);
  if (o.after) fail("no more pages");
  const state = o.state ?? "open";
  if (!["open", "closed", "all"].includes(state)) fail(`invalid argument "${state}" for "-s, --state" flag: valid values are {open|closed|all}`);
  let items = search(world, o.search ?? "", { kinds: ["discussion"] }).map((h) => h.item);
  if (state !== "all") items = items.filter((d) => d.closed === (state === "closed"));
  if (o.category) {
    const c = o.category.toLowerCase();
    if (!world.discussions.some((d) => d.category.name.toLowerCase() === c || d.category.slug === c)) fail(`could not find category "${o.category}"`);
    items = items.filter((d) => d.category.name.toLowerCase() === c || d.category.slug === c);
  }
  for (const l of (o.label ?? []).flatMap((x) => x.split(","))) items = items.filter((d) => d.labels.some((x) => x.name === l));
  if (o.author) items = items.filter((d) => d.author?.login === o.author);
  if (o.answered) items = items.filter((d) => d.answered);
  if (!o.search || o.sort) {
    const key = o.sort === "created" ? "createdAt" : "updatedAt";
    items.sort((a, b) => Date.parse(b[key]) - Date.parse(a[key]));
    if (o.order === "asc") items.reverse();
  }
  const out = items.slice(0, limitOf(o, 30)).map((d) => discussionJSON(d, undefined));
  emit(o, DISCUSSION_LIST_FIELDS, out, () =>
    out.map((d) => `${d.number}\t${d.state}\t${d.title}\t${d.category.name}\t${labelsCell(d)}\t\t${d.updatedAt}\n`).join(""),
  );
}

function replyThread(world, ref, o) {
  const id = ref.includes("#discussioncomment-") ? null : ref;
  for (const d of world.discussions) {
    for (const c of d.comments.nodes) {
      if (c.id === id || (id === null && c.url === ref)) return { d, c };
    }
  }
  fail(`GraphQL: Could not resolve to a node with the global id of '${ref}'`);
}

function discussionView(world, argv) {
  const { values: o, positionals } = args(argv, {
    comments: { type: "boolean", short: "c" },
    order: { type: "string" },
    after: { type: "string" },
  });
  quiOnly(o.repo);
  if (positionals.length !== 1) fail("accepts 1 arg(s), received " + positionals.length);
  if (o.after) fail("no more pages");
  const ref = positionals[0];
  const limit = limitOf(o, 30);
  const order = o.order ?? "newest";
  if (!["oldest", "newest"].includes(order)) fail(`invalid argument "${order}" for "--order" flag: valid values are {oldest|newest}`);
  const take = (nodes, n) => (order === "newest" ? nodes.slice(-n).reverse() : nodes.slice(0, n));

  if (ref.startsWith("DC_") || ref.includes("#discussioncomment-")) {
    const { d, c } = replyThread(world, ref, o);
    const replies = c.replies.nodes;
    const comments = { nodes: [commentJSON(c, { nodes: take(replies, limit).map((r) => commentJSON(r)), totalCount: replies.length })], totalCount: 1 };
    emit(o, DISCUSSION_VIEW_FIELDS, discussionJSON(d, comments), () =>
      take(replies, limit)
        .reverse()
        .map((r) => `author:\t${r.author?.login ?? "ghost"}\ncreated:\t${r.createdAt}\nurl:\t${r.url}\n--\n${r.body}\n--\n`)
        .join(""),
    );
    return;
  }

  const n = Number(ref.replace(/^#/, "").replace(/^https:\/\/github\.com\/autobrr\/qui\/discussions\//, ""));
  const d = world.discussions.find((x) => x.number === n);
  if (!d) fail(`GraphQL: Could not resolve to a Discussion with the number of ${ref}. (repository.discussion)`);
  const shown = take(d.comments.nodes, limit);
  const comments = {
    nodes: shown.map((c) => commentJSON(c, { nodes: c.replies.nodes.slice(-4).reverse().map((r) => commentJSON(r)), totalCount: c.replies.totalCount })),
    totalCount: d.comments.totalCount,
  };
  emit(o, DISCUSSION_VIEW_FIELDS, discussionJSON(d, comments), () => {
    if (o.comments) {
      return shown.map((c) => `author:\t${c.author?.login ?? "ghost"}\ncreated:\t${c.createdAt}\nurl:\t${c.url}\n--\n${c.body}\n--\n`).join("");
    }
    return [
      `title:\t${d.title}`,
      `state:\t${d.closed ? "CLOSED" : "OPEN"}`,
      `category:\t${d.category.name}`,
      `author:\t${d.author?.login ?? "ghost"}`,
      `labels:\t${d.labels.map((l) => l.name).sort().join(", ")}`,
      `comments:\t${d.comments.totalCount}`,
      `number:\t${d.number}`,
      `url:\t${d.url}`,
      "--",
      `${d.body}\n`,
    ].join("\n");
  });
}

function searchIssues(world, argv, kind) {
  const prOpts = kind === "pr" ? { merged: { type: "boolean" }, "merged-at": { type: "string" }, draft: { type: "boolean" }, base: { type: "string", short: "B" }, head: { type: "string", short: "H" } } : { "include-prs": { type: "boolean" } };
  const { values: o, positionals } = args(argv, {
    ...prOpts,
    state: { type: "string" },
    label: { type: "string", multiple: true },
    author: { type: "string" },
    app: { type: "string" },
    match: { type: "string", multiple: true },
    sort: { type: "string" },
    order: { type: "string" },
    created: { type: "string" },
    closed: { type: "string" },
    updated: { type: "string" },
    "no-label": { type: "boolean" },
    owner: { type: "string" },
  });
  const quals = [];
  const repo = o.repo ? repoOf(o.repo) : REPO;
  if (repo !== REPO) {
    emit(o, kind === "pr" ? SEARCH_PR_FIELDS : SEARCH_ISSUE_FIELDS, [], () => "");
    return;
  }
  if (o.state) quals.push(`is:${o.state}`);
  if (o.merged) quals.push("is:merged");
  if (o["merged-at"]) quals.push(`merged:${o["merged-at"]}`);
  for (const l of o.label ?? []) quals.push(`label:"${l}"`);
  if (o.author) quals.push(`author:${o.author}`);
  if (o.app) quals.push(`author:app/${o.app}`);
  if (o.created) quals.push(`created:${o.created}`);
  if (o.closed) quals.push(`closed:${o.closed}`);
  if (o.updated) quals.push(`updated:${o.updated}`);
  if (o["no-label"]) quals.push("no:label");
  if (o.match) quals.push(`in:${o.match.join(",")}`);
  const query = `${positionals.join(" ")} ${quals.join(" ")}`;
  if (!parseQuery(query).groups.length && !quals.length && !positionals.length) fail("specify search keywords or flags");
  const kinds = kind === "pr" ? ["pr"] : o["include-prs"] ? ["issue", "pr"] : ["issue"];
  let hits = search(world, query, { kinds });
  if (o.sort === "created" || o.sort === "updated") {
    const key = `${o.sort}At`;
    hits.sort((a, b) => Date.parse(b.item[key]) - Date.parse(a.item[key]));
    if (o.order === "asc") hits.reverse();
  }
  hits = hits.slice(0, limitOf(o, 30));
  const items = hits.map((h) => searchJSON(h.item, h.kind));
  emit(o, kind === "pr" ? SEARCH_PR_FIELDS : SEARCH_ISSUE_FIELDS, items, () =>
    items.map((i) => `${REPO}\t${i.number}\t${i.state}\t${i.title}\t${labelsCell(i)}\t${i.updatedAt}\n`).join(""),
  );
}

function git(dir, gitArgs) {
  return execFileSync("git", ["-C", dir, ...gitArgs], { encoding: "utf8", maxBuffer: 64 * 1024 * 1024, stdio: ["ignore", "pipe", "pipe"] });
}

function searchCode(world, argv) {
  const { values: o, positionals } = args(argv, {
    extension: { type: "string" },
    filename: { type: "string" },
    language: { type: "string" },
    match: { type: "string", multiple: true },
    owner: { type: "string" },
    size: { type: "string" },
  });
  const parsed = parseQuery(positionals.join(" "));
  const repoQ = parsed.qualifiers.find((q) => q.key === "repo");
  const repo = repoOf(o.repo ?? repoQ?.value);
  const code = world.code[repo];
  if (!code) fail(`code search for ${repo} is not available`);
  if (!parsed.groups.length) fail("specify search keywords or flags");
  const grep = (term) => {
    let out = "";
    try {
      out = git(code.dir, ["grep", "-l", "-i", "-F", "-I", "-e", term, code.commit]);
    } catch (err) {
      if (err.status !== 1) throw err;
    }
    return out.split("\n").filter(Boolean).map((l) => l.slice(code.commit.length + 1));
  };
  let files = null;
  for (const group of parsed.groups) {
    const set = new Set(group.flatMap(grep));
    files = files ? new Set([...files].filter((f) => set.has(f))) : set;
  }
  for (const f of parsed.exclude.flatMap(grep)) files.delete(f);
  const terms = parsed.groups.flat();
  const pathQ = parsed.qualifiers.filter((q) => ["path", "filename", "extension"].includes(q.key));
  if (o.filename) pathQ.push({ key: "filename", value: o.filename });
  if (o.extension) pathQ.push({ key: "extension", value: o.extension });
  const keep = (f) =>
    pathQ.every((q) => (q.key === "path" ? f.includes(q.value) : q.key === "filename" ? f.split("/").pop().includes(q.value) : f.endsWith(`.${q.value}`)));
  const list = [...files].filter(keep).sort().slice(0, limitOf(o, 30));
  const nameWithOwner = repo === QBIT ? "qbittorrent/qBittorrent" : REPO;
  const results = list.map((path) => {
    const lines = git(code.dir, ["show", `${code.commit}:${path}`]).split("\n");
    const fragment = lines.find((l) => terms.some((t) => l.toLowerCase().includes(t))) ?? "";
    return {
      path,
      repository: { id: "", isFork: false, isPrivate: false, nameWithOwner, url: `https://github.com/${nameWithOwner}` },
      sha: "",
      textMatches: [{ fragment, matches: [], property: "content", type: "FileContent" }],
      url: `https://github.com/${nameWithOwner}/blob/${code.commit}/${path}`,
    };
  });
  emit(o, ["path", "repository", "sha", "textMatches", "url"], results, () =>
    results.map((r) => `${nameWithOwner}:${r.path}: ${r.textMatches[0].fragment.trim()}\n`).join(""),
  );
}

// Only the call shape that .github/scripts/qbit-source.sh makes.
function api(world, argv) {
  const { values: o, positionals } = args(argv, {
    method: { type: "string", short: "X" },
    "raw-field": { type: "string", short: "f", multiple: true },
    header: { type: "string", short: "H", multiple: true },
  });
  const m = /^repos\/qbittorrent\/qBittorrent\/contents\/(.+)$/.exec(positionals[0] ?? "");
  const ref = (o["raw-field"] ?? []).find((f) => f.startsWith("ref="))?.slice(4);
  if (!m || !ref || (o.method && o.method !== "GET")) fail("gh api is not available here");
  const code = world.code[QBIT];
  if (!code) fail("the eval run has no qBittorrent clone (see --qbit in run.mjs)");
  let tagDate;
  try {
    tagDate = git(code.dir, ["log", "-1", "--format=%cI", `refs/tags/${ref}`]).trim();
  } catch {
    fail("gh: No commit found for the ref " + ref + " (HTTP 404)");
  }
  if (Date.parse(tagDate) > Date.parse(world.at)) fail("gh: No commit found for the ref " + ref + " (HTTP 404)");
  try {
    process.stdout.write(git(code.dir, ["show", `refs/tags/${ref}:${m[1]}`]));
  } catch {
    fail("gh: Not Found (HTTP 404)");
  }
}

const commands = {
  "label list": labelList,
  "release list": releaseList,
  "issue view": issueView,
  "issue list": issueList,
  "discussion view": discussionView,
  "discussion list": discussionList,
  "search issues": (w, a) => searchIssues(w, a, "issue"),
  "search prs": (w, a) => searchIssues(w, a, "pr"),
  "search code": searchCode,
};

export function main(argv, world) {
  const [group, sub, ...rest] = argv;
  if (group === "api") return api(world, argv.slice(1));
  const cmd = commands[`${group} ${sub === "ls" ? "list" : sub}`];
  if (!cmd) fail(`gh ${argv.slice(0, 2).join(" ")} is not available here`);
  cmd(world, rest);
}

process.stdout.on("error", (err) => {
  if (err.code === "EPIPE") process.exit(0);
  throw err;
});

try {
  const world = JSON.parse(readFileSync(process.env.QUI_EVAL_WORLD, "utf8"));
  main(process.argv.slice(2), world);
} catch (err) {
  if (!(err instanceof GhError)) throw err;
  process.stderr.write(`${err.message}\n`);
  process.exitCode = 1;
}

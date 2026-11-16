import assert from "node:assert/strict";
import { test } from "node:test";
import { cutWorld, parseQuery, search } from "./world.mjs";

const label = (name, createdAt = "2026-01-01T00:00:00Z") => ({ id: `L_${name}`, name, color: "000000", description: "", createdAt });
const user = { login: "reporter" };
const cmt = (createdAt, body, replies) => ({ id: `C_${createdAt}`, url: "u", author: user, body, createdAt, ...(replies && { replies: { totalCount: replies.length, nodes: replies } }) });
const category = { name: "Issue Triage", slug: "issue-triage" };

const corpus = {
  labels: [label("bug"), label("needs-info"), label("ready-for-agent"), label("rls", "2026-11-01T00:00:00Z")],
  discussions: [
    {
      number: 10,
      title: "Torrents stuck on Moving",
      body: "qui shows moving",
      category,
      createdAt: "2026-10-01T00:00:00Z",
      closed: true,
      closedAt: "2026-11-15T00:00:00Z",
      stateReason: "OUTDATED",
      labels: { nodes: [label("bug"), label("needs-info"), label("rls")] },
      comments: {
        nodes: [
          cmt("2026-10-01T01:00:00Z", "triage asks", [cmt("2026-10-01T02:00:00Z", "answer"), cmt("2026-10-20T00:00:00Z", "late reply")]),
          cmt("2026-11-15T00:00:00Z", "stale close"),
        ],
      },
    },
    { number: 20, title: "Later report", body: "", category, createdAt: "2026-10-05T00:00:00Z", closed: false, closedAt: null, labels: { nodes: [] }, comments: { nodes: [] } },
  ],
  issues: [
    {
      number: 5,
      title: "Old issue about moving",
      body: "",
      createdAt: "2026-09-01T00:00:00Z",
      closed: true,
      closedAt: "2026-10-10T00:00:00Z",
      stateReason: "COMPLETED",
      labels: { nodes: [label("bug")] },
      comments: { nodes: [cmt("2026-09-02T00:00:00Z", "early"), cmt("2026-10-09T00:00:00Z", "late")] },
      timelineItems: {
        nodes: [
          { __typename: "LabeledEvent", createdAt: "2026-09-01T00:00:00Z", label: { name: "needs-info" } },
          { __typename: "UnlabeledEvent", createdAt: "2026-10-09T00:00:00Z", label: { name: "needs-info" } },
          { __typename: "LabeledEvent", createdAt: "2026-10-09T00:00:00Z", label: { name: "bug" } },
        ],
      },
    },
    {
      number: 30,
      title: "Issue that triage opened later",
      body: "From discussion #10",
      createdAt: "2026-10-02T00:00:00Z",
      closed: false,
      closedAt: null,
      labels: { nodes: [label("ready-for-agent")] },
      comments: { nodes: [] },
      timelineItems: { nodes: [] },
    },
  ],
  pulls: [
    { number: 7, title: "fix moving state", body: "Fixes #5", createdAt: "2026-09-20T00:00:00Z", closed: true, closedAt: "2026-10-09T00:00:00Z", mergedAt: "2026-10-09T00:00:00Z", labels: { nodes: [] }, comments: { totalCount: 0 } },
  ],
  releasesQui: [
    { tagName: "v1.0.0", publishedAt: "2026-09-01T00:00:00Z", isDraft: false, isPrerelease: false },
    { tagName: "v1.1.0", publishedAt: "2026-10-03T00:00:00Z", isDraft: false, isPrerelease: false },
  ],
  releasesQbittorrent: [],
};

const at = "2026-10-01T03:00:00Z";
const world = cutWorld(corpus, { at, discussion: 10, labels: ["bug", "needs-info"] });

test("the world holds nothing created after the moment", () => {
  assert.deepEqual(world.discussions.map((d) => d.number), [10]);
  assert.deepEqual(world.issues.map((i) => i.number), [5]);
  assert.deepEqual(world.releases["autobrr/qui"].map((r) => r.tagName), ["v1.0.0"]);
  assert.deepEqual(world.labels.map((l) => l.name), ["bug", "needs-info", "ready-for-agent"]);
});

test("the discussion under test loses later comments, its close, and gets the case labels", () => {
  const d = world.discussions[0];
  assert.equal(d.closed, false);
  assert.equal(d.closedAt, null);
  assert.equal(d.stateReason, null);
  assert.equal(d.updatedAt, "2026-10-01T02:00:00Z");
  assert.deepEqual(d.labels.map((l) => l.name), ["bug", "needs-info"]);
  assert.equal(d.comments.totalCount, 1);
  assert.deepEqual(d.comments.nodes[0].replies.nodes.map((r) => r.body), ["answer"]);
});

test("a body given for the moment replaces the edited body", () => {
  const w = cutWorld(corpus, { at, discussion: 10, labels: [], body: "original" });
  assert.equal(w.discussions[0].body, "original");
  assert.equal(world.discussions[0].body, "qui shows moving");
});

// regression: #4224
test("each body shows its text at the moment, not a later edit", () => {
  const edits = (...pairs) => pairs.map(([editedAt, body]) => ({ editedAt, body }));
  const [d, d20] = corpus.discussions;
  const [ask] = d.comments.nodes;
  const [reply] = ask.replies.nodes;
  const [issue] = corpus.issues;
  const [early] = issue.comments.nodes;
  const edited = {
    ...corpus,
    discussions: [
      {
        ...d,
        body: "qui shows moving, edited",
        edits: edits([d.createdAt, "qui shows moving"], ["2026-10-02T00:00:00Z", "qui shows moving, edited"]),
        comments: { nodes: [{ ...ask, replies: { nodes: [{ ...reply, body: "answer, and the log", edits: edits([reply.createdAt, "answer"], ["2026-10-01T04:00:00Z", "answer, and the log"]) }] } }] },
      },
      d20,
    ],
    issues: [{ ...issue, comments: { nodes: [{ ...early, body: "early, edited", edits: edits([early.createdAt, "early"], ["2026-10-05T00:00:00Z", "early, edited"]) }] } }],
    pulls: [{ ...corpus.pulls[0], body: "Fixes #5", edits: edits([corpus.pulls[0].createdAt, "WIP"], ["2026-10-05T00:00:00Z", "Fixes #5"]) }],
  };
  const w = cutWorld(edited, { at, discussion: 10, labels: [] });
  assert.equal(w.discussions[0].body, "qui shows moving");
  assert.equal(w.discussions[0].comments.nodes[0].replies.nodes[0].body, "answer");
  assert.equal(w.issues[0].comments.nodes[0].body, "early");
  assert.equal(w.pulls[0].body, "WIP");
  assert.equal(w.discussions[0].edits, undefined);
});

test("an issue gets the labels and the state of the moment", () => {
  const i = world.issues[0];
  assert.equal(i.state, "OPEN");
  assert.equal(i.closed, false);
  assert.deepEqual(i.labels.map((l) => l.name), ["needs-info"]);
  assert.deepEqual(i.comments.nodes.map((c) => c.body), ["early"]);
});

test("a pull request merged after the moment is open", () => {
  assert.equal(world.pulls[0].state, "OPEN");
  assert.equal(world.pulls[0].mergedAt, null);
  const later = cutWorld(corpus, { at: "2026-10-09T12:00:00Z", discussion: 10, labels: [] });
  assert.equal(later.pulls[0].state, "MERGED");
});

test("a case label that did not exist yet is an error", () => {
  assert.throws(() => cutWorld(corpus, { at, discussion: 10, labels: ["rls"] }), /labels did not exist/);
  assert.throws(() => cutWorld(corpus, { at, discussion: 20, labels: [] }), /did not exist/);
});

test("parseQuery splits terms, OR groups, exclusions, and qualifiers", () => {
  const q = parseQuery('moving OR relocate "stuck state" -seeding is:closed label:"needs-info"');
  assert.deepEqual(q.groups, [["moving", "relocate"], ["stuck state"]]);
  assert.deepEqual(q.exclude, ["seeding"]);
  assert.deepEqual(q.qualifiers.map((x) => `${x.key}:${x.value}`), ["is:closed", "label:needs-info"]);
});

test("search matches stems, applies qualifiers, and sees only the cut world", () => {
  const hits = (q, kinds) => search(world, q, { kinds }).map((h) => `${h.kind}#${h.item.number}`);
  assert.deepEqual(hits("move", ["issue", "discussion"]), ["discussion#10", "issue#5"]);
  assert.deepEqual(hits("moving is:open", ["issue"]), ["issue#5"]);
  assert.deepEqual(hits("moving is:closed", ["issue"]), []);
  assert.deepEqual(hits("From discussion", ["issue"]), []);
  assert.deepEqual(hits("answer", ["discussion"]), ["discussion#10"]);
  assert.deepEqual(hits("late reply", ["discussion"]), []);
  assert.deepEqual(hits("moving is:merged", ["pr"]), []);
});

// regression: #4224
test("type:pull-request finds pull requests", () => {
  assert.deepEqual(search(world, "type:pull-request moving", { kinds: ["pr"] }).map((h) => h.item.number), [7]);
});

// regression: #4224
test("an answer and an edit count only from their moment", () => {
  const [d, d20] = corpus.discussions;
  const [ask] = d.comments.nodes;
  const answered = {
    ...d,
    answerChosenAt: "2026-10-01T02:30:00Z",
    answerChosenBy: user,
    edits: [{ editedAt: d.createdAt, body: d.body }, { editedAt: "2026-10-01T02:45:00Z", body: d.body }],
    comments: {
      nodes: [
        {
          ...ask,
          edits: [{ editedAt: ask.createdAt, body: ask.body }, { editedAt: "2026-10-01T02:50:00Z", body: ask.body }],
          replies: { nodes: [{ ...ask.replies.nodes[0], isAnswer: true, edits: [{ editedAt: "2026-10-01T02:00:00Z", body: "answer" }, { editedAt: "2026-10-05T00:00:00Z", body: "answer" }] }] },
        },
      ],
    },
  };
  const c = { ...corpus, discussions: [answered, d20] };
  const before = cutWorld(c, { at: "2026-10-01T02:15:00Z", discussion: 10, labels: [] }).discussions[0];
  assert.equal(before.answered, false);
  assert.equal(before.comments.nodes[0].replies.nodes[0].isAnswer, false);
  assert.equal(before.updatedAt, "2026-10-01T02:00:00Z");
  const w = cutWorld(c, { at, discussion: 10, labels: [] });
  const after = w.discussions[0];
  assert.equal(after.answered, true);
  assert.equal(after.comments.nodes[0].replies.nodes[0].isAnswer, true);
  assert.equal(after.updatedAt, "2026-10-01T02:50:00Z");
  assert.deepEqual(search(w, "is:answered", { kinds: ["discussion"] }).map((h) => h.item.number), [10]);
});

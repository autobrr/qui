// Saves a read-only snapshot of autobrr/qui (discussions, issues, pull
// requests, labels, releases) and the qBittorrent releases, for the triage
// eval. It holds today's data: world.mjs cuts it back to the moment of a case.
//
// Usage: node snapshot.mjs <evals-dir>
// Writes <evals-dir>/corpus/*.json. Needs a signed-in gh. Sends only queries.
import { execFileSync } from "node:child_process";
import { mkdirSync, renameSync, writeFileSync } from "node:fs";
import { join } from "node:path";

const evalsDir = process.argv[2];
if (!evalsDir) {
  console.error("usage: node snapshot.mjs <evals-dir>");
  process.exit(2);
}
const corpusDir = join(evalsDir, "corpus");
mkdirSync(corpusDir, { recursive: true });

function graphql(query, vars = {}) {
  const args = ["api", "graphql", "-f", `query=${query}`];
  for (const [k, v] of Object.entries(vars)) {
    if (v === null || v === undefined) continue;
    args.push(typeof v === "number" ? "-F" : "-f", `${k}=${v}`);
  }
  const out = execFileSync("gh", args, { maxBuffer: 512 * 1024 * 1024, encoding: "utf8" });
  return JSON.parse(out).data;
}

function paginate(label, connection, query, pageSize) {
  const nodes = [];
  let after = null;
  for (;;) {
    const data = graphql(query, { n: pageSize, after });
    const conn = connection(data);
    nodes.push(...conn.nodes);
    process.stderr.write(`\r${label}: ${nodes.length}/${conn.totalCount}`);
    if (!conn.pageInfo.hasNextPage) break;
    after = conn.pageInfo.endCursor;
  }
  process.stderr.write("\n");
  return nodes;
}

const LABELS = "labels(first:50){nodes{id name color description}}";
const AUTHOR = "author{login ... on User{id name} ... on Bot{id}}";
const COMMENT = `id url ${AUTHOR} authorAssociation body createdAt`;

const discussions = paginate(
  "discussions",
  (d) => d.repository.discussions,
  `query($n:Int!,$after:String){repository(owner:"autobrr",name:"qui"){discussions(first:$n,after:$after,orderBy:{field:CREATED_AT,direction:ASC}){totalCount pageInfo{hasNextPage endCursor}
    nodes{id number title body url createdAt updatedAt closed closedAt stateReason locked ${AUTHOR} authorAssociation
      category{id name slug emoji isAnswerable} ${LABELS}
      comments(first:100){totalCount nodes{${COMMENT} isAnswer upvoteCount
        replies(first:100){totalCount nodes{${COMMENT} isAnswer upvoteCount}}}}}}}}`,
  20,
);

const issues = paginate(
  "issues",
  (d) => d.repository.issues,
  `query($n:Int!,$after:String){repository(owner:"autobrr",name:"qui"){issues(first:$n,after:$after,orderBy:{field:CREATED_AT,direction:ASC}){totalCount pageInfo{hasNextPage endCursor}
    nodes{id number title body url createdAt updatedAt closed closedAt state stateReason locked ${AUTHOR} authorAssociation ${LABELS}
      comments(first:100){totalCount nodes{${COMMENT}}}
      timelineItems(first:100,itemTypes:[LABELED_EVENT,UNLABELED_EVENT]){nodes{__typename ... on LabeledEvent{createdAt label{name}} ... on UnlabeledEvent{createdAt label{name}}}}}}}}`,
  50,
);

const pulls = paginate(
  "pull requests",
  (d) => d.repository.pullRequests,
  `query($n:Int!,$after:String){repository(owner:"autobrr",name:"qui"){pullRequests(first:$n,after:$after,orderBy:{field:CREATED_AT,direction:ASC}){totalCount pageInfo{hasNextPage endCursor}
    nodes{id number title body url createdAt updatedAt closed closedAt mergedAt state isDraft locked ${AUTHOR} authorAssociation ${LABELS}
      comments{totalCount}}}}}`,
  50,
);

const labels = paginate(
  "labels",
  (d) => d.repository.labels,
  `query($n:Int!,$after:String){repository(owner:"autobrr",name:"qui"){labels(first:$n,after:$after){totalCount pageInfo{hasNextPage endCursor} nodes{id name color description createdAt}}}}`,
  100,
);

const releaseQuery = (owner, name) =>
  `query($n:Int!,$after:String){repository(owner:"${owner}",name:"${name}"){releases(first:$n,after:$after,orderBy:{field:CREATED_AT,direction:DESC}){totalCount pageInfo{hasNextPage endCursor} nodes{name tagName createdAt publishedAt isDraft isPrerelease}}}}`;
const quiReleases = paginate("qui releases", (d) => d.repository.releases, releaseQuery("autobrr", "qui"), 100);
const qbitReleases = paginate(
  "qBittorrent releases",
  (d) => d.repository.releases,
  releaseQuery("qbittorrent", "qBittorrent"),
  100,
);

const fetchedAt = new Date().toISOString();
const files = {
  "discussions.json": discussions,
  "issues.json": issues,
  "pulls.json": pulls,
  "labels.json": labels,
  "releases-qui.json": quiReleases,
  "releases-qbittorrent.json": qbitReleases,
  "meta.json": { fetchedAt },
};
for (const [name, data] of Object.entries(files)) {
  const path = join(corpusDir, name);
  writeFileSync(`${path}.tmp`, JSON.stringify(data));
  renameSync(`${path}.tmp`, path);
}
console.log(`snapshot saved to ${corpusDir} at ${fetchedAt}`);

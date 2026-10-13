import { fromMarkdown } from "mdast-util-from-markdown";
import { toString } from "mdast-util-to-string";

type Node = ReturnType<typeof fromMarkdown>["children"][number];

const CHANGELOG_SECTIONS: Record<string, [string, string]> = {
  "New Features": ["new feature", "new features"],
  "Bug Fixes": ["bug fix", "bug fixes"],
  "Other Changes": ["other change", "other changes"],
};

// The text of the bold run that starts a paragraph, if it starts with one.
function leadingBold(node: { type: string; children?: unknown[] } | undefined): string | undefined {
  if (node?.type !== "paragraph") return undefined;
  const first = node.children?.[0] as Node | undefined;
  return first?.type === "strong" ? toString(first).trim() : undefined;
}

// releaseBody makes the Discord card text for a release post: the Breaking or
// Important notice, the Highlights titles, and the number of entries in each
// changelog section. A post without Highlights gets only the notice and counts.
export function releaseBody(markdown: string): string {
  const notices: string[] = [];
  const highlights: string[] = [];
  const counts: string[] = [];

  let section = "";
  let sectionNodes: Node[] = [];
  const flush = () => {
    const notice = section.match(/breaking|important/i)?.[0];
    if (notice && sectionNodes.length > 0) {
      const label = notice.toLowerCase() === "breaking" ? "Breaking" : "Important";
      const first = sectionNodes[0];
      const text = leadingBold(first.type === "list" ? first.children[0]?.children[0] : first) ?? toString(first).trim().split(/(?<=[.!?])\s/)[0];
      notices.push(`**${label}:** ${text}`);
    }
    if (section === "Highlights") {
      for (const node of sectionNodes) {
        if (node.type !== "list") continue;
        for (const item of node.children) {
          const title = leadingBold(item.children[0]);
          if (title) highlights.push(`- ${title}`);
        }
      }
    }
    const words = CHANGELOG_SECTIONS[section];
    if (words) {
      const n = sectionNodes.reduce((sum, node) => sum + (node.type === "list" ? node.children.length : 0), 0);
      if (n > 0) counts.push(`${n} ${n === 1 ? words[0] : words[1]}`);
    }
  };

  for (const node of fromMarkdown(markdown).children) {
    if (node.type === "heading") {
      flush();
      section = toString(node).trim();
      sectionNodes = [];
    } else {
      sectionNodes.push(node);
    }
  }
  flush();

  const countLine = counts.join(" · ");
  const tail = highlights.length > 0 && countLine ? `-# ${countLine}` : countLine;
  return [...notices, ...highlights, tail].filter(Boolean).join("\n");
}

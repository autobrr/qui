import Metadata from "@theme-original/BlogPostPage/Metadata";
import { useBlogPost } from "@docusaurus/plugin-content-blog/client";
import type { ReactNode } from "react";
import DiscordEmbed from "@site/src/components/DiscordEmbed";

const dateFormat = new Intl.DateTimeFormat("en-US", { dateStyle: "long", timeZone: "UTC" });

export default function BlogPostPageMetadata(): ReactNode {
  const { metadata } = useBlogPost();

  return (
    <>
      <Metadata />
      <DiscordEmbed
        source={`qui release notes · ${dateFormat.format(new Date(metadata.date))}`}
        title={metadata.title}
        body={metadata.frontMatter.discord_embed_body as string | undefined}
        permalink={metadata.permalink}
        buttonLabel="Open release notes"
      />
    </>
  );
}

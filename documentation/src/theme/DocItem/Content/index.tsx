import clsx from "clsx";
import { ThemeClassNames } from "@docusaurus/theme-common";
import { useDoc } from "@docusaurus/plugin-content-docs/client";
import Heading from "@theme/Heading";
import MDXContent from "@theme/MDXContent";
import type { ReactNode } from "react";
import type { Props } from "@theme/DocItem/Content";
import OpenInAI from "@site/src/components/OpenInAI";
import DiscordEmbed from "@site/src/components/DiscordEmbed";
import styles from "./styles.module.css";

function useSyntheticTitle(): string | null {
  const { metadata, frontMatter, contentTitle } = useDoc();
  const shouldRender = !frontMatter.hide_title && typeof contentTitle === "undefined";

  if (!shouldRender) {
    return null;
  }

  return metadata.title;
}

export default function DocItemContent({ children }: Props): ReactNode {
  const syntheticTitle = useSyntheticTitle();
  const { metadata } = useDoc();

  return (
    <div className={clsx(ThemeClassNames.docs.docMarkdown, "markdown")}>
      <DiscordEmbed
        source="qui docs"
        title={metadata.title}
        body={metadata.description}
        permalink={metadata.permalink}
        buttonLabel="Open docs"
      />
      <div className={styles.actionsRow}>
        <OpenInAI />
      </div>
      {syntheticTitle && (
        <header>
          <Heading as="h1">{syntheticTitle}</Heading>
        </header>
      )}
      <MDXContent>{children}</MDXContent>
    </div>
  );
}

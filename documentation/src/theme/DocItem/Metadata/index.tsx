import Head from "@docusaurus/Head";
import Metadata from "@theme-original/DocItem/Metadata";
import { useDoc } from "@docusaurus/plugin-content-docs/client";
import type { ReactNode } from "react";

export default function DocItemMetadata(): ReactNode {
  const { metadata, frontMatter } = useDoc();

  return (
    <>
      <Metadata />
      {frontMatter.title_no_suffix === true && (
        <Head>
          <title>{metadata.title}</title>
          <meta property="og:title" content={metadata.title} />
        </Head>
      )}
    </>
  );
}

import Head from "@docusaurus/Head";
import useBaseUrl from "@docusaurus/useBaseUrl";
import useDocusaurusContext from "@docusaurus/useDocusaurusContext";
import type { ReactNode } from "react";

type Props = {
  source: string;
  title: string;
  body?: string;
  permalink: string;
  buttonLabel: string;
};

// DiscordEmbed puts a Discord component embed in the page <head>. Discord shows
// it as the link preview instead of the Open Graph card. The payload limit is
// 3,000 bytes. See https://docs.discord.com/developers/link-previews/component-embeds
export default function DiscordEmbed({ source, title, body, permalink, buttonLabel }: Props): ReactNode {
  const { siteConfig } = useDocusaurusContext();
  const image = useBaseUrl("/img/discord-banner.webp", { absolute: true });
  const url = siteConfig.url + permalink;

  const payload = {
    component: {
      type: 17,
      accent_color: 0xe5e5e5,
      components: [
        { type: 12, items: [{ media: { url: image } }] },
        { type: 10, content: [`-# ${source}`, `## ${title}`, body].filter(Boolean).join("\n") },
        { type: 14 },
        {
          type: 1,
          components: [
            { type: 2, style: 5, label: buttonLabel, url },
            { type: 2, style: 5, label: "Discord", url: "https://discord.autobrr.com/qui" },
          ],
        },
      ],
    },
  };

  return (
    <Head>
      <script id="discord:component-embed" type="application/json">
        {JSON.stringify(payload)}
      </script>
    </Head>
  );
}

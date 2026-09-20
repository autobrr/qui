import Link from "@docusaurus/Link";
import Head from "@docusaurus/Head";
import useDocusaurusContext from "@docusaurus/useDocusaurusContext";
import { useWindowSize } from "@docusaurus/theme-common";
import Layout from "@theme/Layout";
import type { ReactNode } from "react";
import { ExternalArrowIcon } from "../components/OpenInAI";
import styles from "./index.module.css";

// Below 1024px the app shows its phone chrome, which does not fit in a framed
// box on a phone. Those visitors get a poster that opens the demo in its own tab.
// The poster is also the server-rendered state, so a phone never fetches the iframe.
function Demo() {
  const desktop = useWindowSize({ desktopBreakpoint: 1023 }) === "desktop";
  return (
    <div className={styles.demo}>
      {desktop ? (
        <iframe className={styles.demoFrame} src="/demo/" title="qui demo" />
      ) : (
        <Link className={styles.demoPoster} href="/demo/" target="_blank" rel="noopener">
          <img
            src="/img/qui-hero.webp"
            width={1400}
            height={840}
            alt="The qui torrent table with sidebar filters, categories, and live stats"
          />
          <span className={styles.demoPosterButton}>Open the demo</span>
        </Link>
      )}
      <p className={styles.demoNote}>
        Nothing you do in the demo is saved.{" "}
        <Link href="/demo/" target="_blank" rel="noopener">
          Open it in a new tab
        </Link>
      </p>
    </div>
  );
}

const features = [
  {
    title: "Multi-instance",
    body: "Add each qBittorrent once. Switch from the sidebar and keep one login for all of them, with optional OIDC.",
    link: "/docs/features/instance-settings",
  },
  {
    title: "Cross-seed",
    body: "Find matching torrents across your trackers and add them automatically.",
    link: "/docs/features/cross-seed/overview",
  },
  {
    title: "Automations",
    body: "Rules with conditions and actions manage torrents while you sleep.",
    link: "/docs/features/automations",
  },
  {
    title: "Reverse proxy",
    body: "A qBittorrent-compatible endpoint per instance, so Sonarr, Radarr, and autobrr point at qui instead of each client.",
    link: "/docs/features/reverse-proxy",
  },
  {
    title: "Backups",
    body: "Scheduled snapshots of each instance, with incremental and full restore.",
    link: "/docs/features/backups",
  },
  {
    title: "Orphan scan",
    body: "Find files on disk that no torrent references, and clean them up.",
    link: "/docs/features/orphan-scan",
  },
];

export default function Home(): ReactNode {
  const { siteConfig } = useDocusaurusContext();
  return (
    <Layout title="Fast web UI for qBittorrent" description={siteConfig.tagline}>
      <Head>
        <script type="application/ld+json">
          {JSON.stringify({
            "@context": "https://schema.org",
            "@type": "SoftwareApplication",
            name: "qui",
            applicationCategory: "UtilitiesApplication",
            operatingSystem: "Linux, Windows, macOS, Docker",
            description: siteConfig.tagline,
            url: siteConfig.url,
            downloadUrl: "https://github.com/autobrr/qui/releases",
            softwareHelp: `${siteConfig.url}/docs/intro/`,
            license: "https://www.gnu.org/licenses/gpl-2.0.html",
            offers: { "@type": "Offer", price: "0", priceCurrency: "USD" },
            author: { "@type": "Organization", name: "autobrr", url: "https://github.com/autobrr" },
            sameAs: ["https://github.com/autobrr/qui", "https://discord.autobrr.com/qui"],
          })}
        </script>
      </Head>
      <main className={styles.main}>
        <header className={styles.top}>
          <div className={styles.brand}>
            <span className={styles.title}>qui</span>
            <h1 className={styles.tagline}>A fast web UI for qBittorrent</h1>
          </div>
          <div className={styles.actions}>
            <Link className={styles.buttonPrimary} to="/docs/getting-started/installation">
              Get started
            </Link>
            <Link className={styles.buttonSecondary} href="https://github.com/autobrr/qui">
              GitHub
            </Link>
          </div>
        </header>
        <Demo />
        <ul className={styles.features}>
          {features.map((f) => (
            <li key={f.title}>
              <Link to={f.link}>
                {f.title}
                <ExternalArrowIcon />
              </Link>
              <p>{f.body}</p>
            </li>
          ))}
        </ul>
        <div className={styles.about}>
          <p>
            qui is a web UI for qBittorrent. Add each instance once and manage all of them from one
            page. The torrent table is virtualized and updates arrive over server-sent events, so ten
            thousand torrents or more scroll, filter, and sort without lag. Indexer search, RSS, and
            notifications are built in.
          </p>
          <p>
            Download one binary or run the Docker image on Linux, macOS, or Windows. SQLite by default,
            Postgres when you want it. Free and open source under GPL-2.0-or-later, made by the{" "}
            <Link href="https://github.com/autobrr">autobrr</Link> team. The demo above runs the real
            interface on synthetic data.
          </p>
          <div className={styles.actions}>
            <Link className={styles.buttonPrimary} to="/docs/getting-started/installation">
              Get started
            </Link>
            <Link className={styles.buttonSecondary} to="/docs/intro">
              Read the docs
            </Link>
          </div>
        </div>
      </main>
    </Layout>
  );
}

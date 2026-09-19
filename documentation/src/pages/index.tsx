import Link from "@docusaurus/Link";
import Head from "@docusaurus/Head";
import useDocusaurusContext from "@docusaurus/useDocusaurusContext";
import Layout from "@theme/Layout";
import type { ReactNode } from "react";
import styles from "./index.module.css";

function Demo() {
  return (
    <div className={styles.demo}>
      <iframe
        className={styles.demoFrame}
        src="/demo/"
        title="qui demo"
        loading="lazy"
      />
      <p className={styles.demoNote}>
        Nothing you do here is saved.{" "}
        <Link href="/demo/" target="_blank" rel="noopener">
          Open the demo in a new tab
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
            <h1 className={styles.title}>qui</h1>
            <p className={styles.tagline}>A fast web UI for qBittorrent</p>
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
              <Link to={f.link}>{f.title}</Link>
              <p>{f.body}</p>
            </li>
          ))}
        </ul>
        <p className={styles.facts}>
          <span>Single binary or Docker</span>
          <span>SQLite or Postgres</span>
          <span>Linux, macOS, Windows</span>
        </p>
      </main>
    </Layout>
  );
}

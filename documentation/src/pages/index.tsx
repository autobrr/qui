import Link from "@docusaurus/Link";
import useDocusaurusContext from "@docusaurus/useDocusaurusContext";
import Layout from "@theme/Layout";
import { useRef, useState, type ReactNode } from "react";
import styles from "./index.module.css";

function SectionLabel({ children }: { children: string }) {
  return <span className={styles.sectionLabel}>( {children} )</span>;
}

function HeroSection() {
  return (
    <header className={styles.hero}>
      <div className={styles.heroBlob} aria-hidden="true" />
      <div className={styles.heroContent}>
        <SectionLabel>QBITTORRENT WEB UI</SectionLabel>
        <h1 className={styles.heroTitle}>
          <span className={styles.accent}>One interface</span> for every
          <br />
          qBittorrent instance
        </h1>
        <p className={styles.heroTagline}>
          qui connects to all your qBittorrent instances and stays fast past
          your ten thousandth torrent. A single binary with cross-seed,
          automations, and backups built in.
        </p>
        <div className={styles.heroButtons}>
          <Link
            className={styles.buttonPrimary}
            to="/docs/getting-started/installation"
          >
            Get started
          </Link>
          <Link className={styles.buttonSecondary} href="https://github.com/autobrr/qui">
            GitHub
          </Link>
        </div>
      </div>
      <ul className={styles.statStrip}>
        <li>Single binary</li>
        <li>SQLite or Postgres</li>
        <li>Docker or bare metal</li>
        <li>9+ languages</li>
      </ul>
    </header>
  );
}

function ScreenshotSection() {
  return (
    <section className={styles.screenshot}>
      <img
        src="/img/qui-hero.png"
        alt="The qui torrent table with sidebar filters, categories, and live stats"
        className={styles.screenshotImage}
      />
    </section>
  );
}

const whyItems = [
  {
    title: "Every instance in one place",
    body: "Add each qBittorrent instance once. Switch from the sidebar and keep one login for all of them, with optional OIDC.",
  },
  {
    title: "A single binary",
    body: "Download one file and run it. SQLite by default, Postgres when you want it. Nothing else to install.",
  },
  {
    title: "Fast where it counts",
    body: "The torrent table is virtualized and updates arrive over server-sent events. Large collections scroll, filter, and sort without lag.",
  },
  {
    title: "A drop-in proxy",
    body: "qui exposes a transparent qBittorrent-compatible endpoint per instance. Point your other tools at qui instead of at each client.",
  },
];

function WhySection() {
  return (
    <section className={styles.split}>
      <div className={styles.splitRail}>
        <SectionLabel>WHY QUI</SectionLabel>
      </div>
      <div className={styles.splitBody}>
        <h2 className={styles.sectionTitle}>
          Built for a <span className={styles.accent}>serious</span> library
        </h2>
        <div className={styles.accordion}>
          {whyItems.map((item, i) => (
            <details
              key={item.title}
              name="why-qui"
              className={styles.accordionItem}
              open={i === 0}
            >
              <summary className={styles.accordionSummary}>{item.title}</summary>
              <p className={styles.accordionBody}>{item.body}</p>
            </details>
          ))}
        </div>
      </div>
    </section>
  );
}

const features = [
  {
    title: "Cross-seed",
    description: "Find matching torrents across your trackers and add them automatically.",
    link: "/docs/features/cross-seed/overview",
  },
  {
    title: "Automations",
    description: "Rules with conditions and actions manage torrents while you sleep.",
    link: "/docs/features/automations",
  },
  {
    title: "Backups",
    description: "Scheduled snapshots of each instance, with incremental and full restore.",
    link: "/docs/features/backups",
  },
  {
    title: "Orphan scan",
    description: "Find files on disk that no torrent references, and clean them up.",
    link: "/docs/features/orphan-scan",
  },
];

function FeaturesSection() {
  return (
    <section className={styles.split}>
      <div className={styles.splitRail}>
        <SectionLabel>IN THE BOX</SectionLabel>
      </div>
      <div className={styles.splitBody}>
        <h2 className={styles.sectionTitle}>
          More than a <span className={styles.accent}>remote control</span>
        </h2>
        <div className={styles.featuresGrid}>
          {features.map((feature, i) => (
            <Link key={feature.title} to={feature.link} className={styles.featureCard}>
              <span className={styles.featureIndex}>
                {String(i + 1).padStart(2, "0")}
              </span>
              <h3 className={styles.featureTitle}>{feature.title}</h3>
              <p className={styles.featureDescription}>{feature.description}</p>
              <span className={styles.featureMore}>Read more</span>
            </Link>
          ))}
        </div>
      </div>
    </section>
  );
}

function CopyButton({ getText }: { getText: () => string }) {
  const [copied, setCopied] = useState(false);
  return (
    <button
      type="button"
      className={styles.copyButton}
      onClick={() => {
        navigator.clipboard.writeText(getText());
        setCopied(true);
        setTimeout(() => setCopied(false), 1600);
      }}
    >
      {copied ? "Copied" : "Copy"}
    </button>
  );
}

function QuickStartSection() {
  const preRef = useRef<HTMLPreElement>(null);
  return (
    <section className={styles.quickStart}>
      <div className={styles.quickStartBlob} aria-hidden="true" />
      <div className={styles.quickStartInner}>
        <div className={styles.splitRail}>
          <SectionLabel>QUICK START</SectionLabel>
        </div>
        <div className={styles.splitBody}>
          <h2 className={styles.sectionTitle}>
            Running in <span className={styles.accent}>one minute</span>
          </h2>
          <div className={styles.codeFrame}>
            <div className={styles.codeHeader}>
              <span>docker</span>
              <CopyButton getText={() => preRef.current?.textContent ?? ""} />
            </div>
            {/* Hand-tinted: the band is dark in both themes, so the
                theme-following Prism CodeBlock cannot be used here. */}
            <pre ref={preRef} className={styles.codeBlock}>
              docker run <span className={styles.tokFlag}>-d</span>{" "}
              <span className={styles.tokDim}>\</span>
              {"\n  "}
              <span className={styles.tokFlag}>-p</span>{" "}
              <span className={styles.tokVal}>7476:7476</span>{" "}
              <span className={styles.tokDim}>\</span>
              {"\n  "}
              <span className={styles.tokFlag}>-v</span>{" "}
              <span className={styles.tokVal}>$(pwd)/config:/config</span>{" "}
              <span className={styles.tokDim}>\</span>
              {"\n  "}ghcr.io/autobrr/qui:latest
            </pre>
          </div>
          <p className={styles.quickStartNote}>
            Then open <code>http://localhost:7476</code> and add your first
            instance. The{" "}
            <Link to="/docs/getting-started/installation">install guide</Link>{" "}
            covers binaries, Docker, and seedbox installers for Linux, Windows,
            and macOS.
          </p>
        </div>
      </div>
    </section>
  );
}

function CommunitySection() {
  return (
    <section className={styles.split}>
      <div className={styles.splitRail}>
        <SectionLabel>COMMUNITY</SectionLabel>
      </div>
      <div className={styles.splitBody}>
        <h2 className={styles.sectionTitle}>
          Built in <span className={styles.accent}>the open</span>
        </h2>
        <p className={styles.communityText}>
          qui is free and open source, made by the autobrr team. Ask in
          Discord and get an answer fast.
        </p>
        <div className={styles.heroButtons}>
          <Link className={styles.buttonPrimary} href="https://discord.autobrr.com/qui">
            Discord
          </Link>
          <Link className={styles.buttonSecondary} to="/docs/intro">
            Read the docs
          </Link>
        </div>
      </div>
    </section>
  );
}

export default function Home(): ReactNode {
  const { siteConfig } = useDocusaurusContext();
  return (
    <Layout title={siteConfig.title} description={siteConfig.tagline}>
      <main className={styles.main}>
        <HeroSection />
        <ScreenshotSection />
        <WhySection />
        <FeaturesSection />
        <QuickStartSection />
        <CommunitySection />
      </main>
    </Layout>
  );
}

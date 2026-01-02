import Link from "@docusaurus/Link";
import useDocusaurusContext from "@docusaurus/useDocusaurusContext";
import Layout from "@theme/Layout";
import type { ReactNode } from "react";
import styles from "./index.module.css";

function HeroSection() {
  return (
    <header className={styles.hero}>
      <div className={styles.heroContent}>
        <img
          src="/img/qui.png"
          alt="qui logo"
          className={styles.heroLogo}
        />
        <h1 className={styles.heroTitle}>qui</h1>
        <p className={styles.heroTagline}>
          Modern web interface for qBittorrent
        </p>
        <div className={styles.heroButtons}>
          <Link
            className={styles.buttonPrimary}
            to="/docs/getting-started/installation"
          >
            Get Started
          </Link>
          <Link
            className={styles.buttonSecondary}
            href="https://github.com/autobrr/qui"
          >
            <GithubIcon />
            GitHub
          </Link>
        </div>
      </div>
    </header>
  );
}

function ScreenshotSection() {
  return (
    <section className={styles.screenshot}>
      <div className={styles.screenshotContainer}>
        <img
          src="/img/qui-hero.png"
          alt="qui interface screenshot"
          className={styles.screenshotImage}
        />
      </div>
    </section>
  );
}

type FeatureItem = {
  title: string;
  description: string;
  icon: ReactNode;
  link: string;
};

const features: FeatureItem[] = [
  {
    title: "Multi-Instance",
    description: "Manage all your qBittorrent instances from one place",
    icon: <ServerIcon />,
    link: "/docs/intro",
  },
  {
    title: "Cross-Seed",
    description: "Automatically find and add matching torrents across trackers",
    icon: <PuzzleIcon />,
    link: "/docs/features/cross-seed/overview",
  },
  {
    title: "Automations",
    description: "Rule-based torrent management with conditions and actions",
    icon: <GearIcon />,
    link: "/docs/features/automations",
  },
  {
    title: "Backups",
    description: "Scheduled snapshots with incremental and complete restore",
    icon: <ShieldIcon />,
    link: "/docs/features/backups",
  },
];

function FeatureCard({ title, description, icon, link }: FeatureItem) {
  return (
    <Link to={link} className={styles.featureCard}>
      <div className={styles.featureIcon}>{icon}</div>
      <h3 className={styles.featureTitle}>{title}</h3>
      <p className={styles.featureDescription}>{description}</p>
    </Link>
  );
}

function FeaturesSection() {
  return (
    <section className={styles.features}>
      <div className={styles.featuresGrid}>
        {features.map((feature) => (
          <FeatureCard key={feature.title} {...feature} />
        ))}
      </div>
    </section>
  );
}

export default function Home(): ReactNode {
  const { siteConfig } = useDocusaurusContext();
  return (
    <Layout
      title={siteConfig.title}
      description={siteConfig.tagline}
    >
      <main className={styles.main}>
        <HeroSection />
        <FeaturesSection />
        <ScreenshotSection />
      </main>
    </Layout>
  );
}

// Icons (inline SVG for no dependencies)

function GithubIcon() {
  return (
    <svg
      className={styles.buttonIcon}
      viewBox="0 0 24 24"
      fill="currentColor"
      aria-hidden="true"
    >
      <path d="M12 0c-6.626 0-12 5.373-12 12 0 5.302 3.438 9.8 8.207 11.387.599.111.793-.261.793-.577v-2.234c-3.338.726-4.033-1.416-4.033-1.416-.546-1.387-1.333-1.756-1.333-1.756-1.089-.745.083-.729.083-.729 1.205.084 1.839 1.237 1.839 1.237 1.07 1.834 2.807 1.304 3.492.997.107-.775.418-1.305.762-1.604-2.665-.305-5.467-1.334-5.467-5.931 0-1.311.469-2.381 1.236-3.221-.124-.303-.535-1.524.117-3.176 0 0 1.008-.322 3.301 1.23.957-.266 1.983-.399 3.003-.404 1.02.005 2.047.138 3.006.404 2.291-1.552 3.297-1.23 3.297-1.23.653 1.653.242 2.874.118 3.176.77.84 1.235 1.911 1.235 3.221 0 4.609-2.807 5.624-5.479 5.921.43.372.823 1.102.823 2.222v3.293c0 .319.192.694.801.576 4.765-1.589 8.199-6.086 8.199-11.386 0-6.627-5.373-12-12-12z" />
    </svg>
  );
}

function ServerIcon() {
  return (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <rect x="2" y="2" width="20" height="8" rx="2" ry="2" />
      <rect x="2" y="14" width="20" height="8" rx="2" ry="2" />
      <line x1="6" y1="6" x2="6.01" y2="6" />
      <line x1="6" y1="18" x2="6.01" y2="18" />
    </svg>
  );
}

function PuzzleIcon() {
  return (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <path d="M19.439 7.85c-.049.322.059.648.289.878l1.568 1.568c.47.47.706 1.087.706 1.704s-.235 1.233-.706 1.704l-1.611 1.611a.98.98 0 0 1-.837.276c-.47-.07-.802-.48-.968-.925-.247-.665-.67-1.167-1.376-1.167-.97 0-1.758.787-1.758 1.758 0 .706-.502 1.159-1.167 1.376-.445.166-.855.498-.925.968a.98.98 0 0 1-.276.837l-1.61 1.611a2.404 2.404 0 0 1-1.705.707 2.402 2.402 0 0 1-1.704-.707l-1.568-1.568a1.026 1.026 0 0 0-.877-.29c-.493.074-.84.504-1.02.968-.267.685-.71 1.199-1.448 1.199-.97 0-1.758-.788-1.758-1.758 0-.738.514-1.181 1.199-1.449.464-.18.894-.526.968-1.02a1.026 1.026 0 0 0-.29-.876l-1.567-1.568A2.402 2.402 0 0 1 1.998 12c0-.617.236-1.234.707-1.705L4.315 8.683a.98.98 0 0 1 .837-.276c.47.07.802.48.968.925.247.665.67 1.167 1.376 1.167.97 0 1.758-.787 1.758-1.758 0-.706.502-1.159 1.167-1.376.445-.166.855-.498.925-.968a.98.98 0 0 1 .276-.837l1.611-1.611a2.404 2.404 0 0 1 3.409 0l1.568 1.568c.23.23.556.338.877.29.493-.074.84-.504 1.02-.968.267-.686.71-1.2 1.448-1.2.97 0 1.759.788 1.759 1.758 0 .738-.515 1.182-1.2 1.449-.464.18-.894.527-.967 1.02Z" />
    </svg>
  );
}

function GearIcon() {
  return (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <path d="M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z" />
      <circle cx="12" cy="12" r="3" />
    </svg>
  );
}

function ShieldIcon() {
  return (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <path d="M20 13c0 5-3.5 7.5-7.66 8.95a1 1 0 0 1-.67-.01C7.5 20.5 4 18 4 13V6a1 1 0 0 1 1-1c2 0 4.5-1.2 6.24-2.72a1.17 1.17 0 0 1 1.52 0C14.51 3.81 17 5 19 5a1 1 0 0 1 1 1z" />
      <path d="m9 12 2 2 4-4" />
    </svg>
  );
}

import clsx from "clsx";
import { useDoc } from "@docusaurus/plugin-content-docs/client";
import { useLocation } from "@docusaurus/router";
import useDocusaurusContext from "@docusaurus/useDocusaurusContext";
import type { ReactNode } from "react";
import { useEffect, useMemo, useRef, useState } from "react";
import styles from "./styles.module.css";

type CopyState = "idle" | "copying" | "copied" | "error";

type AIProvider = {
  id: string;
  label: string;
  href: string;
  icon: ReactNode;
};

const AI_PROVIDERS: AIProvider[] = [
  {
    id: "chatgpt",
    label: "Open in ChatGPT",
    href: "https://chatgpt.com/?hints=search&q=",
    icon: <ChatGPTIcon />,
  },
  {
    id: "claude",
    label: "Open in Claude",
    href: "https://claude.ai/new?q=",
    icon: <ClaudeIcon />,
  },
  {
    id: "perplexity",
    label: "Open in Perplexity",
    href: "https://www.perplexity.ai/?q=",
    icon: <PerplexityIcon />,
  },
  {
    // gemini.google.com ignores a prompt in the URL, so link to Google AI Mode, which takes one.
    id: "google-ai-mode",
    label: "Open in Google AI Mode",
    href: "https://www.google.com/search?udm=50&q=",
    icon: <SparkleIcon />,
  },
];

function normalizeBaseUrl(url: string, baseUrl: string): URL {
  const origin = url.endsWith("/") ? url : `${url}/`;
  return new URL(baseUrl, origin);
}

async function copyText(value: string): Promise<void> {
  if (navigator.clipboard?.writeText) {
    await navigator.clipboard.writeText(value);
    return;
  }

  const textArea = document.createElement("textarea");
  textArea.value = value;
  textArea.style.position = "fixed";
  textArea.style.opacity = "0";
  document.body.appendChild(textArea);
  textArea.focus();
  textArea.select();
  document.execCommand("copy");
  document.body.removeChild(textArea);
}

function getPrompt(title: string, pageUrl: string, markdownUrl: string): string {
  return [
    "Answer questions about this qui docs page.",
    `Title: ${title}`,
    `Page URL: ${pageUrl}`,
    `Markdown source: ${markdownUrl}`,
    "Use this page as the source of truth.",
  ].join("\n");
}

export default function OpenInAI(): ReactNode {
  const { metadata } = useDoc();
  const location = useLocation();
  const { siteConfig } = useDocusaurusContext();
  const [isOpen, setIsOpen] = useState(false);
  const [copyState, setCopyState] = useState<CopyState>("idle");
  const containerRef = useRef<HTMLDivElement>(null);

  const pageUrl = useMemo(() => {
    if (typeof window !== "undefined") {
      return window.location.href;
    }

    const base = normalizeBaseUrl(siteConfig.url, siteConfig.baseUrl);
    return new URL(location.pathname.replace(/^\//, ""), base).toString();
  }, [location.pathname, siteConfig.baseUrl, siteConfig.url]);

  // docusaurus-plugin-llms writes a Markdown copy of each page to <page path>.md at build time.
  const markdownUrl = useMemo(
    () => new URL(`${location.pathname.replace(/\/$/, "")}.md`, pageUrl).toString(),
    [location.pathname, pageUrl],
  );

  const prompt = useMemo(
    () => getPrompt(metadata.title, pageUrl, markdownUrl),
    [markdownUrl, metadata.title, pageUrl],
  );

  const copyPage = async () => {
    setCopyState("copying");
    try {
      const response = await fetch(markdownUrl);
      if (!response.ok) {
        throw new Error(`Unable to fetch markdown (${response.status})`);
      }
      await copyText(await response.text());
      setCopyState("copied");
      setIsOpen(false);
    } catch (error) {
      console.error(error);
      setCopyState("error");
    }
  };

  useEffect(() => {
    if (copyState === "idle" || copyState === "copying") {
      return;
    }

    const timer = window.setTimeout(() => setCopyState("idle"), 1600);
    return () => window.clearTimeout(timer);
  }, [copyState]);

  useEffect(() => {
    if (!isOpen) {
      return;
    }

    const onPointerDown = (event: MouseEvent) => {
      if (!containerRef.current?.contains(event.target as Node)) {
        setIsOpen(false);
      }
    };

    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        setIsOpen(false);
      }
    };

    document.addEventListener("mousedown", onPointerDown);
    document.addEventListener("keydown", onKeyDown);

    return () => {
      document.removeEventListener("mousedown", onPointerDown);
      document.removeEventListener("keydown", onKeyDown);
    };
  }, [isOpen]);

  const copyLabel =
    copyState === "copying"
      ? "Copying..."
      : copyState === "copied"
        ? "Copied"
        : copyState === "error"
          ? "Retry copy"
          : "Copy page";

  const openMarkdown = () => {
    window.open(markdownUrl, "_blank", "noopener,noreferrer");
    setIsOpen(false);
  };

  return (
    <div className={styles.container} ref={containerRef}>
      <div className={styles.splitButton}>
        <button
          type="button"
          onClick={copyPage}
          className={styles.primaryButton}
          aria-label="Copy page as markdown"
        >
          <CopyIcon />
          <span>{copyLabel}</span>
        </button>
        <button
          type="button"
          onClick={() => setIsOpen((value) => !value)}
          className={clsx(styles.chevronButton, isOpen && styles.chevronOpen)}
          aria-haspopup="menu"
          aria-expanded={isOpen}
          aria-label="Open AI actions"
        >
          <ChevronIcon />
        </button>
      </div>

      {isOpen && (
        <div className={styles.menu} role="menu" aria-label="AI actions">
          <button type="button" role="menuitem" className={styles.menuItem} onClick={copyPage}>
            <span className={styles.menuIcon}>
              <CopyIcon />
            </span>
            <span className={styles.menuText}>
              <strong>Copy page</strong>
              <small>Copy page as Markdown for LLMs</small>
            </span>
          </button>

          <button
            type="button"
            role="menuitem"
            className={styles.menuItem}
            onClick={openMarkdown}
          >
            <span className={styles.menuIcon}>
              <MarkdownIcon />
            </span>
            <span className={styles.menuText}>
              <strong>View as Markdown</strong>
              <small>View this page as plain text</small>
            </span>
            <ExternalArrowIcon />
          </button>

          {AI_PROVIDERS.map((provider) => (
            // A real link, not window.open: phones hand a tapped link to the installed app.
            <a
              key={provider.id}
              href={`${provider.href}${encodeURIComponent(prompt)}`}
              target="_blank"
              rel="noopener noreferrer"
              role="menuitem"
              className={styles.menuItem}
              onClick={() => setIsOpen(false)}
            >
              <span className={styles.menuIcon}>{provider.icon}</span>
              <span className={styles.menuText}>
                <strong>{provider.label}</strong>
                <small>Ask questions about this page</small>
              </span>
              <ExternalArrowIcon />
            </a>
          ))}
        </div>
      )}
    </div>
  );
}

function CopyIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" aria-hidden="true">
      <rect x="9" y="9" width="11" height="11" rx="2" />
      <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" />
    </svg>
  );
}

function ChevronIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" aria-hidden="true">
      <path d="m6 9 6 6 6-6" />
    </svg>
  );
}

function MarkdownIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" aria-hidden="true">
      <rect x="2.5" y="3.5" width="19" height="17" rx="2.5" />
      <path d="M7 15V9l2.4 2.4L11.8 9v6M14.5 13h3M16 11.5V14.5M18 11.5V14.5" />
    </svg>
  );
}

function ChatGPTIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.4" aria-hidden="true">
      <path d="M12.1 2.4a4.4 4.4 0 0 1 4.38 3.96l2.17 1.25a4.4 4.4 0 0 1 1.6 6l-1.27 2.2a4.4 4.4 0 0 1-4.39 7.63H12.1a4.4 4.4 0 0 1-4.38-3.96L5.55 18.2a4.4 4.4 0 0 1-1.6-6l1.27-2.2A4.4 4.4 0 0 1 9.6 2.4h2.5Z" />
      <path d="m8.9 6.6 6.9 4m-8.2 2.9 6.9 4m0-12-6.9 4m8.2 2.9-6.9 4" />
    </svg>
  );
}

function ClaudeIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" aria-hidden="true">
      <circle cx="12" cy="12" r="9.2" />
      <path d="m12 5.8 1.2 3.5h3.7l-3 2.2 1.1 3.6L12 13l-3 2.1 1.1-3.6-3-2.2h3.7Z" />
    </svg>
  );
}

function PerplexityIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" aria-hidden="true">
      <path d="M12 2.8v18.4M2.8 12h18.4M5.2 5.2l13.6 13.6M18.8 5.2 5.2 18.8" />
      <circle cx="12" cy="12" r="9.2" />
    </svg>
  );
}

function SparkleIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" aria-hidden="true">
      <path d="M12 2.5c.6 4.9 4.6 8.9 9.5 9.5-4.9.6-8.9 4.6-9.5 9.5-.6-4.9-4.6-8.9-9.5-9.5 4.9-.6 8.9-4.6 9.5-9.5Z" />
    </svg>
  );
}

export function ExternalArrowIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" aria-hidden="true">
      <path d="M7 17 17 7M9 7h8v8" />
    </svg>
  );
}

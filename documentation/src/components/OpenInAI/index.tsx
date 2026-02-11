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
];

function normalizeBaseUrl(url: string, baseUrl: string): URL {
  const origin = url.endsWith("/") ? url : `${url}/`;
  return new URL(baseUrl, origin);
}

function toRawMarkdownUrl(editUrl?: string, source?: string): string | null {
  if (editUrl) {
    const match = editUrl.match(/^https:\/\/github\.com\/([^/]+)\/([^/]+)\/tree\/([^/]+)\/(.+)$/);
    if (match) {
      const [, owner, repo, branch, path] = match;
      return `https://raw.githubusercontent.com/${owner}/${repo}/${branch}/${path}`;
    }
  }

  if (source?.startsWith("@site/")) {
    const relativePath = source.replace(/^@site\//, "");
    return `https://raw.githubusercontent.com/autobrr/qui/main/documentation/${relativePath}`;
  }

  return null;
}

function stripFrontMatter(markdown: string): string {
  if (!markdown.startsWith("---")) {
    return markdown;
  }

  const frontMatterMatch = markdown.match(/^---\r?\n[\s\S]*?\r?\n---\r?\n?/);
  if (!frontMatterMatch) {
    return markdown;
  }

  return markdown.slice(frontMatterMatch[0].length).trimStart();
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

function getPrompt(title: string, pageUrl: string, markdownUrl: string | null): string {
  const lines = [
    "Answer questions about this qui docs page.",
    `Title: ${title}`,
    `Page URL: ${pageUrl}`,
  ];

  if (markdownUrl) {
    lines.push(`Markdown source: ${markdownUrl}`);
  }

  lines.push("Use this page as the source of truth.");
  return lines.join("\n");
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

  const markdownUrl = useMemo(
    () => toRawMarkdownUrl(metadata.editUrl, metadata.source),
    [metadata.editUrl, metadata.source],
  );

  const prompt = useMemo(
    () => getPrompt(metadata.title, pageUrl, markdownUrl),
    [markdownUrl, metadata.title, pageUrl],
  );

  const copyPage = async () => {
    if (!markdownUrl) {
      setCopyState("error");
      return;
    }

    setCopyState("copying");
    try {
      const response = await fetch(markdownUrl);
      if (!response.ok) {
        throw new Error(`Unable to fetch markdown (${response.status})`);
      }
      const markdown = stripFrontMatter(await response.text());
      await copyText(markdown);
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
    if (!markdownUrl) {
      return;
    }
    window.open(markdownUrl, "_blank", "noopener,noreferrer");
    setIsOpen(false);
  };

  const openProvider = (href: string) => {
    window.open(`${href}${encodeURIComponent(prompt)}`, "_blank", "noopener,noreferrer");
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
            disabled={!markdownUrl}
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
            <button
              key={provider.id}
              type="button"
              role="menuitem"
              className={styles.menuItem}
              onClick={() => openProvider(provider.href)}
            >
              <span className={styles.menuIcon}>{provider.icon}</span>
              <span className={styles.menuText}>
                <strong>{provider.label}</strong>
                <small>Ask questions about this page</small>
              </span>
              <ExternalArrowIcon />
            </button>
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

function ExternalArrowIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" aria-hidden="true">
      <path d="M7 17 17 7M9 7h8v8" />
    </svg>
  );
}

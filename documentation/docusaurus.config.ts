import type * as Preset from "@docusaurus/preset-classic";
import type { Config } from "@docusaurus/types";
import type { PrismTheme } from "prism-react-renderer";

// Custom minimal light theme
const minimalLightTheme: PrismTheme = {
  plain: {
    color: "#1a1a1a",
    backgroundColor: "#f5f5f5",
  },
  styles: [
    { types: ["comment", "prolog", "doctype", "cdata"], style: { color: "#6b7280" } },
    { types: ["punctuation"], style: { color: "#525252" } },
    { types: ["property", "tag", "boolean", "number", "constant", "symbol"], style: { color: "#0f766e" } },
    { types: ["selector", "attr-name", "string", "char", "builtin"], style: { color: "#4f46e5" } },
    { types: ["operator", "entity", "url"], style: { color: "#525252" } },
    { types: ["atrule", "attr-value", "keyword"], style: { color: "#7c3aed" } },
    { types: ["function", "class-name"], style: { color: "#dc2626" } },
    { types: ["regex", "important", "variable"], style: { color: "#ea580c" } },
  ],
};

// Custom minimal dark theme
const minimalDarkTheme: PrismTheme = {
  plain: {
    color: "#e5e5e5",
    backgroundColor: "#2a2a2a",
  },
  styles: [
    { types: ["comment", "prolog", "doctype", "cdata"], style: { color: "#737373" } },
    { types: ["punctuation"], style: { color: "#a3a3a3" } },
    { types: ["property", "tag", "boolean", "number", "constant", "symbol"], style: { color: "#5eead4" } },
    { types: ["selector", "attr-name", "string", "char", "builtin"], style: { color: "#a5b4fc" } },
    { types: ["operator", "entity", "url"], style: { color: "#a3a3a3" } },
    { types: ["atrule", "attr-value", "keyword"], style: { color: "#c4b5fd" } },
    { types: ["function", "class-name"], style: { color: "#fca5a5" } },
    { types: ["regex", "important", "variable"], style: { color: "#fdba74" } },
  ],
};

const config: Config = {
  title: "qui",
  tagline: "Modern web interface for qBittorrent",
  favicon: "img/favicon.png",

  url: "https://getqui.com",
  baseUrl: "/",

  organizationName: "autobrr",
  projectName: "qui",

  onBrokenLinks: "throw",

  markdown: {
    hooks: {
      onBrokenMarkdownLinks: "warn",
    },
  },

  i18n: {
    defaultLocale: "en",
    locales: ["en"],
  },

  presets: [
    [
      "classic",
      {
        docs: {
          sidebarPath: "./sidebars.ts",
          editUrl: "https://github.com/autobrr/qui/tree/main/documentation/",
          routeBasePath: "docs",
        },
        blog: false,
        theme: {
          customCss: "./src/css/custom.css",
        },
      } satisfies Preset.Options,
    ],
  ],

  themeConfig: {
    image: "img/qui-social-card.png",
    colorMode: {
      defaultMode: "dark",
      respectPrefersColorScheme: true,
    },
    navbar: {
      title: "qui",
      logo: {
        alt: "qui Logo",
        src: "img/qui.png",
      },
      items: [
        {
          type: "docSidebar",
          sidebarId: "docsSidebar",
          position: "left",
          label: "Docs",
        },
        {
          href: "https://github.com/autobrr/qui",
          label: "GitHub",
          position: "right",
        },
      ],
    },
    footer: {
      style: "dark",
      links: [
        {
          title: "Docs",
          items: [
            {
              label: "Getting Started",
              to: "/docs/getting-started/installation",
            },
            {
              label: "Configuration",
              to: "/docs/configuration/environment",
            },
            {
              label: "Features",
              to: "/docs/features/backups",
            },
          ],
        },
        {
          title: "Community",
          items: [
            {
              label: "Discord",
              href: "https://discord.autobrr.com/qui",
            },
            {
              label: "GitHub Issues",
              href: "https://github.com/autobrr/qui/issues",
            },
          ],
        },
        {
          title: "More",
          items: [
            {
              label: "GitHub",
              href: "https://github.com/autobrr/qui",
            },
            {
              label: "Releases",
              href: "https://github.com/autobrr/qui/releases",
            },
          ],
        },
      ],
      copyright: `Copyright ${new Date().getFullYear()} autobrr`,
    },
    prism: {
      theme: minimalLightTheme,
      darkTheme: minimalDarkTheme,
      additionalLanguages: ["bash", "toml", "nginx", "yaml", "json"],
    },
  } satisfies Preset.ThemeConfig,
};

export default config;

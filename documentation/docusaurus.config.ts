import { themes as prismThemes } from "prism-react-renderer";
import type { Config } from "@docusaurus/types";
import type * as Preset from "@docusaurus/preset-classic";

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
          routeBasePath: "/",
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
              to: "/getting-started/installation",
            },
            {
              label: "Configuration",
              to: "/configuration/environment",
            },
            {
              label: "Features",
              to: "/features/backups",
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
      copyright: `Copyright ${new Date().getFullYear()} autobrr. Built with Docusaurus.`,
    },
    prism: {
      theme: prismThemes.github,
      darkTheme: prismThemes.dracula,
      additionalLanguages: ["bash", "toml", "nginx", "yaml", "json"],
    },
  } satisfies Preset.ThemeConfig,
};

export default config;

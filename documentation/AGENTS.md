# CLAUDE.md

Documentation guidelines for qui - a self-hosted tool for managing qBittorrent instances.

## Build Commands

```bash
pnpm install      # Install dependencies
pnpm start        # Dev server with hot reload
pnpm build        # Production build to /build
pnpm serve        # Preview production build
```

## Writing Guidelines

**Audience**: Users running qui on their own hardware. They want to get things working, not read marketing copy.

- Keep it short. If a sentence doesn't help the user accomplish something, remove it.
- Lead with actions. Tell users what to do, then explain why if necessary.
- One concept per page. Split large topics into focused documents.
- No emojis. Ever.
- Avoid repetition. Link to existing docs instead of restating information.

## Document Structure

### Frontmatter

Every doc needs frontmatter:

```yaml
---
sidebar_position: 1
title: Page Title
---
```

For key pages (installation, features overview), add a description for link previews:

```yaml
---
sidebar_position: 1
title: Installation
description: Install qui on Linux, Docker, or via seedbox installers.
---
```

Skip `keywords` - search engines ignore them.

### Categories

Create `_category_.json` for directory groupings:

```json
{
  "label": "Category Name",
  "position": 3
}
```

### Headings

- One `#` heading per page (the title)
- Use `##` for major sections
- Use `###` for subsections
- Keep hierarchy flat when possible

## Markdown Features

### Code Blocks

Always specify the language:

````markdown
```bash
qui serve --config /path/to/config.toml
```

```toml
[server]
host = "0.0.0.0"
port = 8080
```
````

Supported languages: `bash`, `toml`, `nginx`, `yaml`, `json`

### Admonitions

Use sparingly for important callouts:

```markdown
:::note
Additional context that isn't critical.
:::

:::tip
Helpful suggestion to improve the experience.
:::

:::info
Background information users might find useful.
:::

:::warning
Something users should be careful about.
:::

:::danger
This can break things or cause data loss.
:::
```

### Tables

Use tables for configuration options and parameters:

```markdown
| Option | Default | Description |
|--------|---------|-------------|
| `host` | `0.0.0.0` | Bind address |
| `port` | `8080` | HTTP port |
```

### Links

Link to related docs:

```markdown
See [installation](/docs/getting-started/installation) for setup instructions.
```

## Custom Components

### CopyAddress

For commands or values users need to copy:

```tsx
import CopyAddress from "@site/src/components/CopyAddress";

<CopyAddress label="API Key" address="your-api-key-here" />
```

## What to Avoid

- **Marketing language** - No "powerful", "seamless", "blazing fast"
- **Unnecessary background** - Skip history and motivation, get to the point
- **Duplication** - Link to existing docs, don't repeat them
- **Over-explaining** - Trust users to understand basics
- **Emojis** - None

## Adding Packages

Docusaurus supports MDX plugins and Remark/Rehype extensions. If a package genuinely improves documentation clarity or usability, add it. Justify the addition in your commit message.

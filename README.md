# Outcrop

[![CI](https://github.com/sanketsaurav/outcrop/actions/workflows/ci.yml/badge.svg)](https://github.com/sanketsaurav/outcrop/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/sanketsaurav/outcrop?sort=semver)](https://github.com/sanketsaurav/outcrop/releases)
[![License: MIT](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

**Share individual Obsidian notes on your own domain.** Run one command in
Obsidian and the note is live at `https://notes.yourdomain.com/…` — exactly as
it looks in your vault. Edit the note and the public page updates itself.

## Why Outcrop

- **Your domain, your server, your data.** A single small Docker container
  (Go + SQLite, ~15 MB image, one data folder) that you can run anywhere.
- **Perfect fidelity.** Notes are rendered by Obsidian itself, so callouts,
  Mermaid diagrams, syntax highlighting, task lists, and even other plugins'
  output (like Dataview) look right — not a server's approximation of your
  markdown.
- **Sharing you can take back.** Links are unguessable by default, can be
  *rotated* (the old link dies instantly), or gated behind a memorable
  passcode like `amber-falcon-83`.
- **Auto-updating.** Shared notes re-publish a few seconds after you stop
  typing. Images and attachments upload automatically and are deduplicated.
- **Fully yours to style.** The site's CSS, JS, and `<head>` live in the
  plugin's settings — edit a few design tokens or replace the whole thing.
- **Good citizen of the web.** Every page ships real SEO metadata, a
  generated social-preview image, a sitemap, and a visitor-facing
  light/dark/system theme switcher.

Outcrop is for *"I want to hand someone a link to this one note, on my
domain, and keep control of it."*

## Quick start

You need a domain, a machine with Docker, and about ten minutes.

### 1. Deploy the server

```bash
mkdir -p outcrop/data && cd outcrop
sudo chown 65532:65532 data      # the container runs as a non-root user
openssl rand -hex 32             # this is your API key — keep it
```

Create `compose.yaml`:

```yaml
services:
  outcrop:
    image: ghcr.io/sanketsaurav/outcrop:latest
    restart: unless-stopped
    environment:
      API_KEY: ${OUTCROP_API_KEY}
      BASE_URL: https://notes.example.com
      SITE_AUTHOR: Your Name        # optional
      TRUST_PROXY: "1"
    volumes:
      - ./data:/data
    ports:
      - "127.0.0.1:8080:8080"
```

Put TLS in front with any reverse proxy — with [Caddy](https://caddyserver.com)
it's two lines:

```
notes.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

Start it and check it's alive:

```bash
OUTCROP_API_KEY=<your key> docker compose up -d
curl https://notes.example.com/healthz    # → ok
```

### 2. Install the plugin

Until Outcrop is in the community directory, install it with
[BRAT](https://github.com/TfTHacker/obsidian42-brat):

1. In Obsidian, install and enable **BRAT** from Community plugins.
2. BRAT → **Add beta plugin** → `sanketsaurav/outcrop`.
3. Enable **Outcrop**, open its settings, enter your **Server URL** and
   **API key**, and hit **Test connection**. On a fresh server this also
   installs the default theme.

(Manual install: download `manifest.json`, `main.js`, `styles.css` from the
[latest release](https://github.com/sanketsaurav/outcrop/releases) into
`<vault>/.obsidian/plugins/outcrop/`.)

### 3. Share a note

Open any note → command palette → **Outcrop: Share current note**. The link
is on your clipboard. That's it.

## Everyday use

| Command | What it does |
|---|---|
| **Share current note** | Publishes (or updates) the note and copies the link. |
| **Copy share link** / **Copy passcode** | Straight to the clipboard. |
| **Rotate share link** | New URL, old one dies instantly — how you revoke a leaked link. |
| **Protect note with a passcode** | Generates a memorable passcode and gates the page. |
| **Remove passcode** | Opens the note back up. |
| **Unshare current note** | Deletes it from the server. |
| **Update all shared notes** | Re-publishes everything (after theme changes). |
| **Open shared notes list** | A table of every share: copy, open, update, rotate, delete. |
| **Push theme to server** | Deploys your CSS/JS/head edits site-wide. |

Everything is also available from a note's right-click menu, and shared notes
show a `⛰ shared` status-bar item whose click menu has the same actions.
Edits push automatically a few seconds after you stop typing (configurable,
or turn it off).

Wikilinks between shared notes become working public links, and when you
share, unshare, or rotate a note, other shared notes that link to it are
refreshed automatically. Links to unshared notes degrade to plain text.
**Frontmatter is never published.**

### The frontmatter Outcrop uses

The plugin stores share state as note properties, so it survives sync and
renames:

```yaml
share_id: 8fK2…                    # managed by the plugin — don't edit
share_url: https://notes.example.com/V5tGkq2Xw8
share_passcode: amber-falcon-83    # only on protected notes; edit to change, delete to unprotect
```

Optional properties you can set yourself:

```yaml
share_slug: how-i-take-notes   # a pretty URL instead of the random token
share_title: Override title
share_description: Custom description for search/social previews
share_noindex: true            # keep this note out of search engines and the sitemap
```

### Passcodes, honestly

Passcodes are hashed on the server (PBKDF2), attempts are rate-limited, and
rotating a link or changing the passcode kicks out everyone who unlocked it.
At ~23 bits of entropy they're **"not for everyone" protection, not a
secrets vault** — right for a note you're sharing with a few people, wrong
for your tax documents.

## Theming

Three editable pieces in the plugin's settings, served on every public page:

- **Site CSS** — opens with a design-token block (fonts, colors, measure,
  radius; separate light and dark palettes). Most personalization is a few
  lines here. Below the tokens, everything Obsidian emits is styled:
  callouts, task lists, code with syntax colors, tables, embeds, footnotes.
- **Site JS** — powers the footer theme switcher, heading anchors, and
  external-link handling. Extend it freely; the server's security policy
  allows only this file to run.
- **Head snippet** — HTML injected into `<head>`. Google Fonts go here
  (a working example ships commented out); the default theme loads Google
  Sans, Google Sans Code, and Crimson Pro as variable fonts.

Edit → **Push theme to server** → every shared note restyles. For full
control of the page structure, drop a `template.html` into the server's data
folder — the built-in template's contract is documented in
[SPEC.md](SPEC.md).

## SEO & sharing previews

Public notes get canonical URLs, meta descriptions, OpenGraph and Twitter
cards, JSON-LD, and a **generated 1200×630 preview image** from the note's
title — pasting a link into Slack or social media shows a proper card. A
`sitemap.xml` lists your indexable notes; anything marked `share_noindex`
(or protected by a passcode) stays out of it and carries a `noindex` tag.

## Privacy & security

- Sharing is **opt-in per note**. Nothing leaves your vault until you run
  Share, and unsharing deletes the note and garbage-collects its attachments.
- One API key (set by you) gates all writes; it's compared in constant time
  and failed attempts are rate-limited.
- Public pages carry a strict Content-Security-Policy; note HTML is
  sanitized at render time; the only third-party origins ever allowed are
  Google Fonts'.
- Attachment URLs are unguessable content-addressed capability URLs; they're
  not gated by note passcodes.
- TLS is your reverse proxy's job. The server never phones home, tracks
  visitors, or embeds analytics (unless you add some to your theme).

## Server reference

All configuration is environment variables:

| Variable | Default | What it does |
|---|---|---|
| `API_KEY` | — | **Required.** Shared secret between plugin and server (`API_KEY_FILE` works too). |
| `BASE_URL` | — | **Required.** Public origin, e.g. `https://notes.example.com`. |
| `SITE_NAME` | host of `BASE_URL` | Shown in page titles and preview images. |
| `SITE_AUTHOR` | — | Author name for structured data. |
| `OG_ACCENT` | `#6c5ce7` | Accent color in generated preview images. |
| `MAX_NOTE_MB` / `MAX_ASSET_MB` | `2` / `25` | Upload size limits. |
| `TRUST_PROXY` | `0` | Set `1` behind a reverse proxy so rate limits see real client IPs. |
| `LISTEN_ADDR` / `DATA_DIR` | `:8080` / `/data` | Rarely need changing. |

**Backups:** everything lives in the data folder (`outcrop.db` + `blobs/`):
`docker compose stop && tar czf backup.tgz data && docker compose start`.
[Litestream](https://litestream.io) works if you want continuous replication.

**Updating:** `docker compose pull && docker compose up -d`. The database
migrates itself.

## FAQ

**Does it work on mobile?** Yes — the plugin is not desktop-only.

**Can visitors see my vault?** No. Only notes you explicitly share exist on
the server, links to unshared notes are stripped, and frontmatter is never
published.

**What if I delete a shared note from my vault?** The share keeps working
until you delete it — it shows up flagged in the shared-notes list, where
you can remove it.

**Can I keep shared notes out of Google?** Yes — per note with
`share_noindex: true`, or flip the "keep new shares out of search engines"
default in settings if you mostly share unlisted links.

**What doesn't survive publishing?** Content that renders asynchronously
slower than the capture delay (raise it in settings), and math is
best-effort. Everything else you see in reading view is what visitors get.

## Contributing

Bug reports and PRs welcome. [CONTRIBUTING.md](CONTRIBUTING.md) covers the
dev setup, tests, and release process; [SPEC.md](SPEC.md) documents the
architecture and API.

## License

[MIT](LICENSE)

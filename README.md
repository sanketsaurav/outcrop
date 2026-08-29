# Outcrop

Share individual Obsidian notes publicly on **your own domain**.

Outcrop is two things in one repo:

- an **Obsidian plugin** that renders a note exactly the way Obsidian does,
  uploads it (with its images and attachments), and manages your share links —
  including rotation, passcodes, and auto-updates on save;
- a small **Go server** (single Docker image, SQLite, one data volume) that
  serves those notes on your domain with proper SEO metadata, generated OG
  images, and a theme you fully control from the plugin's settings.

An *outcrop* is the part of the bedrock exposed at the surface — the publicly
visible part of your vault.

> Design details and API reference live in [SPEC.md](SPEC.md).

## How it works

1. Run "Share note" on any note. The plugin renders it inside Obsidian
   (callouts, Mermaid, syntax highlighting, plugin output — everything you see),
   uploads the HTML and attachments to your server, and writes `share_id` /
   `share_url` into the note's frontmatter.
2. The server hosts it at `https://notes.example.com/<slug>` — a 10-character
   unguessable token by default, or a pretty custom slug if you set one.
3. Edit the note and it re-publishes automatically (debounced). Rotate the link
   to revoke it, add a passcode to gate it, or unshare to kill it.

## Deploying the server

You need: a domain (e.g. `notes.example.com`), a box with Docker, and a
reverse proxy for TLS (Caddy shown here).

```bash
mkdir -p outcrop/data && cd outcrop
# The container runs as uid 65532 (distroless nonroot):
sudo chown 65532:65532 data
openssl rand -hex 32   # this is your API key
```

`compose.yaml`:

```yaml
services:
  outcrop:
    image: ghcr.io/sanketsaurav/outcrop:latest
    restart: unless-stopped
    environment:
      API_KEY: ${OUTCROP_API_KEY}          # the key you just generated
      BASE_URL: https://notes.example.com
      SITE_NAME: notes.example.com          # optional, used in titles/OG images
      SITE_AUTHOR: Your Name                # optional, JSON-LD author
      TRUST_PROXY: "1"                      # you're behind Caddy
    volumes:
      - ./data:/data
    ports:
      - "127.0.0.1:8080:8080"
```

Caddyfile:

```
notes.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

```bash
OUTCROP_API_KEY=<key> docker compose up -d
curl https://notes.example.com/healthz   # → ok
```

All configuration ([full list in the spec](SPEC.md#61-configuration-env-vars)):
`API_KEY` (or `API_KEY_FILE`), `BASE_URL` — required; `LISTEN_ADDR`,
`DATA_DIR`, `SITE_NAME`, `SITE_AUTHOR`, `OG_ACCENT`, `MAX_NOTE_MB`,
`MAX_ASSET_MB`, `TRUST_PROXY` — optional.

**Backups**: everything lives in the data volume (`outcrop.db` + `blobs/`).
`docker compose stop && tar czf backup.tgz data && docker compose start`.
[Litestream](https://litestream.io) works too if you want continuous replication.

## Installing the plugin

Until it's in the community directory, install with
[BRAT](https://github.com/TfTHacker/obsidian42-brat):

1. Install "BRAT" from Obsidian's community plugins.
2. BRAT → *Add beta plugin* → `sanketsaurav/outcrop`.
3. Enable **Outcrop**, open its settings, set **Server URL** and **API key**,
   hit **Test connection**. On a fresh server this also pushes the default
   theme.

Or manually: grab `manifest.json`, `main.js`, `styles.css` from the latest
plugin release into `<vault>/.obsidian/plugins/outcrop/`.

## Using it

| Command | What it does |
|---|---|
| **Share current note** | Creates the share (or pushes an update), copies the link. |
| **Copy share link** | Clipboard, no network. |
| **Rotate share link** | New URL; the old one is dead. This is how you revoke a leaked link. |
| **Protect note with a passcode** | Generates a memorable passcode (`amber-falcon-83`), stores it in frontmatter, gates the page behind it. |
| **Remove passcode** | Opens the note back up. |
| **Unshare current note** | Deletes it from the server, cleans frontmatter. |
| **Update all shared notes** | Re-publishes everything (use after theme changes). |
| **Open shared notes list** | Table of every share: copy / open / update / rotate / delete, orphan detection. |
| **Push theme to server** | Deploys the CSS/JS/head snippet from settings. |

Shared notes update automatically a few seconds after you stop editing
(configurable, or turn it off). The status bar shows `⛰ shared` on shared
notes — click it for update/copy/rotate/unshare without leaving the keyboard…
fine, the mouse. The same actions live in the file context menu.

### Frontmatter

The plugin owns these properties (prefix configurable):

```yaml
share_id: 8fK2…        # stable identity — don't edit
share_url: https://notes.example.com/V5tGkq2Xw8
share_passcode: amber-falcon-83   # present only on protected notes; delete it (and update) to unprotect
```

And you can set these yourself:

```yaml
share_slug: how-i-take-notes   # pretty URL; applied on next share/update
share_title: Override title
share_description: Custom description for meta/OG tags
share_noindex: true            # keep this note out of search engines & the sitemap
```

Wikilinks to other **shared** notes become working public links (and when you
share/unshare/rotate a note, shared notes linking to it are refreshed).
Wikilinks to unshared notes degrade to plain text. Frontmatter is never
published.

## Theming

Three editable pieces in plugin settings, served on every page:

- **Site CSS** — starts with a design-token block (`--font-text`, `--accent`,
  `--max-width`, light/dark palettes…). Most personalization is a few lines
  here. Below the tokens, the default theme styles everything Obsidian emits:
  callouts, task lists, code (with syntax colors), tables, embeds, footnotes,
  Mermaid, the unlock page.
- **Site JS** — powers the footer theme switcher (light/dark/system, persisted
  per visitor), heading anchors, and external-links-in-new-tabs; extend as you
  like. The CSP allows only this file to execute.
- **Head snippet** — HTML injected into `<head>`. This is where **Google
  Fonts** go; the default contains a commented working example. The server's
  CSP allowlists exactly `fonts.googleapis.com` + `fonts.gstatic.com` and no
  other third-party origin.

Edit → **Push theme** → every shared note restyles. For full page-structure
control, drop a `template.html` into the server's data dir (same variables as
[the built-in template](SPEC.md#64-public-routes)).

## SEO

Public notes get: canonical URL, meta description, OpenGraph + Twitter cards,
`article:published/modified_time`, JSON-LD `Article`, a generated **1200×630
OG image** from the note title, and a `sitemap.xml` linked from `robots.txt`.

Notes marked `share_noindex` (or shared with "keep out of search engines" on)
get a `noindex` meta/header and stay out of the sitemap — use that for
unlisted capability links. Passcode-protected notes are always noindex and
never in the sitemap.

## Security model, honestly

- One API key gates all writes (constant-time compare, fail-closed, rate-limited).
- No CORS, ever — browsers can't be tricked into calling the API; the plugin
  uses Obsidian's native requester.
- Public pages carry a strict CSP; note HTML is sanitized at render time and
  inline scripts can't execute regardless.
- Passcodes are PBKDF2-hashed, rate-limited (5/min/IP per note), and unlock
  cookies die on rotation or passcode change. At ~23 bits of entropy they're
  "not for everyone" protection, not a secrets vault.
- Attachment URLs are content-addressed (SHA-256) capability URLs; they are
  not gated by note passcodes.
- TLS is your reverse proxy's job.

## Development

```bash
make hooks       # one-time: enable pre-commit hooks (gofmt/vet + prettier/eslint on staged files)
make test        # server: gofmt check, vet, tests · plugin: unit tests, lint, typecheck, build
make server-dev  # run the server on :8080 with throwaway config
make plugin-dev  # rebuild plugin on change (symlink the repo into a test vault)
make docker      # build the image locally
```

Layout: `server/` (Go 1.24+, stdlib + modernc sqlite + x/image only),
`plugin/` (TypeScript, esbuild, no runtime deps; vitest for unit tests,
prettier + eslint for style), `manifest.json` at the root (Obsidian
convention). CI runs the same checks on every push/PR.

### Releasing

Both artifacts release from tags, fully automated by GitHub Actions:

```bash
make release-plugin V=0.1.1   # bumps manifest.json/versions.json/package.json, commits, tags 0.1.1
make release-server V=0.1.1   # tags server-v0.1.1
git push origin main --tags   # workflows create the GitHub release + GHCR image
```

Plugin tags are bare semver matching `manifest.json` (the Obsidian
community/BRAT requirement); the workflow refuses a tag that doesn't match.
The release carries `manifest.json`, `main.js`, `styles.css` as assets.
Server tags produce `ghcr.io/sanketsaurav/outcrop:{version,latest}` for
amd64 + arm64.

### Publishing to the Obsidian community directory

1. Ship at least one plugin release (bare semver tag, per above).
2. Fork [obsidianmd/obsidian-releases](https://github.com/obsidianmd/obsidian-releases)
   and add an entry to `community-plugins.json`:
   `{"id": "outcrop", "name": "Outcrop", "author": "Sanket Saurav", "description": …, "repo": "sanketsaurav/outcrop"}`.
3. Open the PR using their template and work through the automated validation
   plus human review (this can take a few weeks). Until it's merged, BRAT
   installs work from the releases directly.

## License

[MIT](LICENSE)

# Outcrop

Outcrop shares individual Obsidian notes on your own domain. One container,
one SQLite database, and an Obsidian plugin. Run "Share note" on any note and
it is live at `https://notes.example.com/<slug>`, rendered the way it looks
in your vault. Edit the note and the page updates itself a few seconds later.

It has the essential primitives for sharing notes:

- share, update, and unshare single notes from the command palette, the file
  menu, or the status bar
- unguessable links by default; custom slugs when you want a readable URL
- link rotation: a rotated link gets a new URL and the old one stops working
- passcode protection with generated memorable passcodes (`amber-falcon-83`)
- images and attachments upload automatically, content-addressed and
  deduplicated
- wikilinks between shared notes become working public links; links to
  unshared notes degrade to plain text
- the site's CSS, JS, and `<head>` are managed from the plugin's settings
- per-note SEO metadata, a generated 1200×630 preview image, and a sitemap
- a visitor-facing light/dark/system theme switcher
- a shared-notes list inside Obsidian: copy, open, update, rotate, delete

## How it works

Two pieces:

1. **The plugin** renders the note with Obsidian's own renderer, so callouts,
   Mermaid, syntax highlighting, and other plugins' output come out the way
   you see them. It then rewrites wikilinks, uploads attachments by content
   hash, sanitizes the HTML, and pushes everything to the server over a
   bearer-authenticated API. Share state (`share_id`, `share_url`) lives in
   the note's frontmatter, so it survives sync and renames. The plugin works
   on desktop and mobile.
2. **The server** is a single Go binary over one SQLite file and a blob
   folder. It serves the public pages, the unlock page for protected notes,
   the preview images, and the sitemap. Only you (through the plugin) can
   write to it; visitors can only read. It must be reachable from the
   internet over HTTPS, behind any reverse proxy.

## Deploy the server

You need a domain and a machine with Docker.

```bash
mkdir -p outcrop/data && cd outcrop
sudo chown 65532:65532 data      # the container runs as a non-root user
openssl rand -hex 32             # this is your API key
```

`compose.yaml`:

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

Put TLS in front with a reverse proxy. With [Caddy](https://caddyserver.com):

```
notes.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

Start it and check it responds:

```bash
OUTCROP_API_KEY=<your key> docker compose up -d
curl https://notes.example.com/healthz    # ok
```

## Install the plugin

Install **Outcrop** from Obsidian's community plugins (Settings → Community
plugins → Browse), enable it, then open its settings, enter the server URL
and API key, and run **Test connection**. On a fresh server this also
installs the default theme.

Manual install: download `manifest.json`, `main.js`, `styles.css` from the
[latest release](https://github.com/sanketsaurav/outcrop/releases) into
`<vault>/.obsidian/plugins/outcrop/`.

## Sharing notes

| Command | What it does |
|---|---|
| Share current note | Publishes (or updates) the note and copies the link |
| Copy share link / Copy passcode | Copies to the clipboard |
| Rotate share link | New URL; the old one stops working |
| Protect note with a passcode | Generates a passcode and gates the page |
| Remove passcode | Opens the note back up |
| Unshare current note | Deletes the share from the server |
| Update all shared notes | Re-publishes everything (useful after theme changes) |
| Open shared notes list | A table of every share with per-row actions |
| Push theme to server | Deploys your CSS/JS/head edits |

Shared notes re-publish automatically a few seconds after you stop editing
(configurable, or turn it off). The same actions are in a note's right-click
menu, and shared notes show a `⛰ shared` status-bar item with a click menu.

When you share, unshare, or rotate a note, other shared notes that link to it
are re-published so their links stay correct. Deleting a shared note from
your vault does not delete the share; it shows up flagged in the shared-notes
list, where you can remove it.

Frontmatter is never published. The plugin stores its state there:

```yaml
share_id: 8fK2…                    # managed by the plugin; don't edit
share_url: https://notes.example.com/V5tGkq2Xw8
share_passcode: amber-falcon-83    # on protected notes; edit to change, delete to unprotect
```

And these are yours to set:

```yaml
share_slug: how-i-take-notes   # readable URL instead of the random token
share_title: Override title
share_description: Custom description for search and link previews
share_noindex: true            # keep this note out of search engines and the sitemap
share_class: justify           # extra CSS classes on this note's body; the theme
                               # defines what they do ("justify" ships by default)
```

### Passcodes

Passcodes are hashed on the server (PBKDF2), unlock attempts are rate-limited
per IP and note, and rotating the link or changing the passcode signs
everyone out. A generated passcode has about 23 bits of entropy: right for a
note you hand to a few people, wrong for secrets.

## Theming

Three editable pieces in the plugin's settings, served on every public page:

- **Site CSS** starts with a design-token block: fonts, colors, measure,
  radius, separate light and dark palettes. Below the tokens, everything
  Obsidian emits is styled: callouts, task lists, code, tables, embeds,
  footnotes.
- **Site JS** runs the footer theme switcher, heading anchors, and
  external-link handling. The server's content-security policy allows only
  this file to execute.
- **Head snippet** is HTML injected into `<head>`. Fonts go here; the default
  theme loads Google Sans, Google Sans Code, and Crimson Pro from Google
  Fonts as variable fonts, and a commented example shows how to swap them.

Edit, then **Push theme to server**; every shared note picks it up. For full
control of the page structure, put a `template.html` in the server's data
folder, starting from the built-in one at
`server/internal/web/templates/page.html`.

## Search engines and link previews

Public notes carry canonical URLs, meta descriptions, OpenGraph and Twitter
tags, JSON-LD, and a preview image generated from the note's title, so a
pasted link unfurls properly in chat and social apps. `sitemap.xml` lists
indexable notes. A note marked `share_noindex` (or protected by a passcode)
is excluded from the sitemap and tagged `noindex`; there is also a setting to
make new shares `noindex` by default if you mostly share unlisted links.

## Security

- Sharing is opt-in per note. Nothing leaves the vault until you share, and
  unsharing deletes the note and garbage-collects its attachments.
- Visitors cannot see your vault: only shared notes exist on the server,
  links to unshared notes are stripped, and frontmatter is never uploaded.
- One API key gates all writes. It is compared in constant time and failed
  attempts are rate-limited.
- The plugin reads the vault's file list to find shared notes by their
  frontmatter, and writes to the clipboard when you copy a link or passcode.
  It never reads the clipboard.
- Public pages have a strict content-security policy; note HTML is sanitized
  at render time. The only third-party origins allowed are Google Fonts'.
- Attachment URLs are content-addressed and unguessable, but they are not
  behind a note's passcode.
- The server sets no cookies except after a passcode unlock, and includes no
  analytics or tracking.

## Configuration

Everything is environment variables on the server:

| Variable | Default | Meaning |
|---|---|---|
| `API_KEY` | required | shared secret between plugin and server (`API_KEY_FILE` works too) |
| `BASE_URL` | required | public origin, e.g. `https://notes.example.com` |
| `SITE_NAME` | host of `BASE_URL` | shown in page titles and preview images |
| `SITE_AUTHOR` | unset | author name in structured data |
| `OG_ACCENT` | `#6c5ce7` | accent color in generated preview images |
| `MAX_NOTE_MB` / `MAX_ASSET_MB` | `2` / `25` | upload size limits |
| `TRUST_PROXY` | `0` | set `1` behind a reverse proxy so rate limits see client IPs |
| `LISTEN_ADDR` / `DATA_DIR` | `:8080` / `/data` | rarely need changing |

## Backups and updating

Everything lives in the data folder: `outcrop.db` and `blobs/`. Back up that
folder and you have backed up everything:

```bash
docker compose stop && tar czf backup.tgz data && docker compose start
```

[Litestream](https://litestream.io) works for continuous replication of the
database file.

To update the server, pull and recreate; the database migrates itself:

```bash
docker compose pull && docker compose up -d
```

Releases also publish `:X.Y.Z` tags to pin instead of `:latest`. The plugin
updates through Obsidian's normal community-plugin updates.

## Troubleshooting

- **The container exits immediately:** `API_KEY` or `BASE_URL` is missing, or
  the data folder is not writable by uid 65532. `docker logs` says which.
- **Dynamic content (Dataview and similar) is missing from a shared note:**
  it rendered slower than the capture delay. Raise "Render delay" in the
  plugin settings and share again. Math is best-effort.
- **A shared note looks unstyled:** the theme was never pushed. Run **Push
  theme to server**, or **Test connection**, which pushes the default on a
  fresh server.
- **Unlock attempts return "too many attempts":** the per-IP limit behind a
  proxy is counting the proxy as one client. Set `TRUST_PROXY=1`.

## Contributing

[CONTRIBUTING.md](CONTRIBUTING.md) covers the development setup, tests, and
the release process.

## License

MIT

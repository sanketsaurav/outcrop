# Changelog

## 0.1.0 (2026-08-29)

First release.

### Features

- Share, update, and unshare single notes from the command palette, the
  file menu, or the status bar; shared notes re-publish automatically a few
  seconds after you stop editing.
- Notes are rendered by Obsidian itself, so callouts, Mermaid, syntax
  highlighting, and other plugins' output publish the way they look in the
  vault. Frontmatter is never published.
- Unguessable links by default, custom slugs via `share_slug`, and link
  rotation that kills the old URL instantly.
- Passcode protection with generated memorable passcodes; unlock attempts
  are rate-limited and rotation signs everyone out.
- Images and attachments upload automatically, content-addressed and
  deduplicated; unused files are garbage-collected.
- Wikilinks between shared notes become working public links and stay
  correct when linked notes are shared, unshared, or rotated.
- Site CSS, JS, and head snippet are managed from the plugin's settings,
  with a token-based default theme, Google Fonts support, and a
  visitor-facing light/dark/system switcher.
- Per-note SEO metadata, a generated 1200×630 link-preview image, and a
  sitemap that respects `share_noindex` and passcodes.
- A shared-notes view inside Obsidian: copy, open, update, rotate, and
  delete every share, with orphan detection.

### Server

- Single Go binary over one SQLite file and a blob folder, shipped as a
  ~15 MB multi-arch Docker image running as a non-root user.
- Bearer-authenticated write API with constant-time key comparison and
  rate-limited failures; strict content-security policy on public pages.
- Configured entirely through environment variables; the database migrates
  itself on upgrade. Backups are one folder.

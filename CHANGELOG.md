# Changelog

## 0.1.3 (2026-08-31)

### Features

- Per-note style variants: a `share_class` frontmatter property adds CSS
  classes to the published note's body (sanitized to plain identifiers).
  The default theme ships a `justify` variant — justified text with
  hyphenation. Define your own variants in the site CSS.

### Fixes

- Paragraph spacing on public pages increased from 0.5em to 0.9em.

### Upgrade notes

- Push the theme once after updating so the `justify` styles (and the new
  spacing) are on your server, then set `share_class: justify` on any note
  you want justified and let it re-publish.

## 0.1.2 (2026-08-29)

### Fixes

- A command callback's promise was left unhandled after the 0.1.1 typing
  changes.
- Default theme: body-link underlines are now drawn with borders instead of
  `text-decoration` longhands — same look, cleaner lint.

## 0.1.1 (2026-08-29)

Addresses the community directory's automated review.

### Fixes

- Release assets now ship with GitHub artifact attestations, so their build
  provenance can be verified.
- Settings now appear in Obsidian's settings search (1.13+) via the
  declarative settings API; the settings tab itself is unchanged.
- Async UI callbacks are properly typed; server responses are typed at the
  API boundary instead of flowing as `any`.
- Default theme: the footer's external-link arrow no longer uses CSS masks,
  the `:has()` selector is gone (tables are wrapped at render time instead),
  and an `!important` was replaced with a more specific selector.

### Upgrade notes

- After updating, run **Push theme to server** and **Update all shared
  notes** once: wide tables now scroll inside a wrapper that both the new
  theme and re-published notes need to agree on.

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

# Contributing to Outcrop

Thanks for helping out. This file covers the development setup, tests, and
the release process.

## Repo layout

```
server/    Go 1.24+ server — stdlib + modernc.org/sqlite + golang.org/x/image only
plugin/    TypeScript Obsidian plugin — esbuild bundle, zero runtime deps
manifest.json, versions.json   Obsidian plugin metadata (repo root, per convention)
.claude/skills/tag-release     release runbook (usable as a Claude Code skill)
```

## Setup

Prereqs: Go 1.24+, Node 22+, Docker (only for image builds).

```bash
git clone https://github.com/sanketsaurav/outcrop && cd outcrop
cd plugin && npm install && cd ..
make hooks       # enable pre-commit hooks: gofmt/vet + prettier/eslint on staged files
```

## Development loop

```bash
make server-dev  # run the server on :8080 with a throwaway key and ./.devdata
make plugin-dev  # rebuild plugin/main.js on change
make test        # everything CI runs: gofmt check, vet, go tests, vitest, eslint, prettier, build
make docker      # build the server image locally
```

To try the plugin in a real vault, symlink `manifest.json`, `plugin/main.js`,
and `plugin/styles.css` into `<vault>/.obsidian/plugins/outcrop/`, point the
plugin at `http://localhost:8080` with the dev key from the Makefile, and
reload Obsidian after rebuilds.

## Style & tests

Formatting and linting are enforced (gofmt; prettier + eslint with tabs), and
the pre-commit hook auto-formats staged files. Server changes should come
with table-driven tests next to the code (`httptest` + a temp SQLite per
test); plugin logic that doesn't need Obsidian belongs in dependency-free
modules (see `src/text.ts`) with vitest tests. CI must be green.

Two invariants worth knowing before touching routes or themes:

- The API and web routers share one mux — `compose_test.go` exists because
  overlapping ServeMux patterns panic at startup.
- The page template's structure (`.note`, `.note-body`, `.note-details`, …)
  is a contract that user themes target — breaking it breaks everyone's CSS.
  The template lives at `server/internal/web/templates/page.html`.

## Releasing

Two artifacts release independently, both fully automated from tags:

| Artifact | Tag | Workflow does |
|---|---|---|
| Plugin | bare semver, e.g. `0.2.0` (must equal `manifest.json`) | GitHub release with `manifest.json`, `main.js`, `styles.css`; notes from the matching `CHANGELOG.md` section |
| Server | `server-v0.2.0` | multi-arch image → `ghcr.io/sanketsaurav/outcrop:{version,latest}` |

The full runbook — preflight, version choice, changelog, verification,
failure recovery — is `.claude/skills/tag-release/SKILL.md` (in Claude Code,
just run `/tag-release`). The short version:

```bash
node scripts/bump-plugin-version.mjs 0.2.0   # syncs manifest.json, versions.json, package.json
# add a "## 0.2.0 (YYYY-MM-DD)" section to CHANGELOG.md
git add -A && git commit -m "Release plugin 0.2.0"
git tag -a 0.2.0 -m "Outcrop plugin 0.2.0"
git push origin main --tags
```

## Obsidian community directory

One-time, after the first release: submit through the
[Obsidian Community directory](https://community.obsidian.md) — sign in with
an Obsidian account, link the GitHub account that owns this repo, and add the
plugin. The directory reads `manifest.json` from the default branch and
installs from the release whose tag matches it; automated review feedback
shows up in the directory and is addressed by shipping a new release. BRAT
installs work in the meantime.

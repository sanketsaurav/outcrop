---
name: tag-release
description: Cut an Outcrop release - preflight, version bump, changelog, tag, push, and watch the release workflows for the Obsidian plugin and/or the server Docker image. Use when the user wants to release, ship, publish, or tag a new version.
---

# Cut an Outcrop release

Outcrop ships **two artifacts with independent versions and tag schemes**:

- **Plugin** — a bare semver tag (`0.2.0`, no `v` prefix: the Obsidian
  community/BRAT convention) triggers `.github/workflows/release-plugin.yml`:
  it builds the plugin, hard-fails unless the tag equals `manifest.json`'s
  version, and creates a GitHub release with `manifest.json`, `main.js`,
  `styles.css` as assets. Release notes come from this tag's `CHANGELOG.md`
  section, falling back to a generated commit list if no section matches.
- **Server** — a `server-vX.Y.Z` tag triggers
  `.github/workflows/release-server.yml`: multi-arch (amd64+arm64) image
  pushed to `ghcr.io/sanketsaurav/outcrop`, tagged `X.Y.Z` and `latest`. No
  GitHub release is created for server tags.

Decide which artifacts are shipping by what changed: `plugin/`,
`manifest.json`, or theme files → plugin release; `server/` → server release;
both → both (tags can point at the same commit).

This repo uses **bump-then-tag**: version bumps and the changelog land on
`main` first, then tags point at that commit. The plugin version lives in
**three places that must agree** — `manifest.json` (`version`),
`versions.json` (an entry mapping the version to `minAppVersion`), and
`plugin/package.json` (`version`). `scripts/bump-plugin-version.mjs` syncs
all three; the release workflow hard-fails if the tag and manifest disagree.
The server has no version file — the tag is the version.

## 1. Preflight

Stop and report (don't tag) if any of these fail:

```sh
git rev-parse --abbrev-ref HEAD     # must be main
git status --porcelain              # must be empty
git fetch origin && git rev-list --count main..origin/main   # must be 0
make test    # server: gofmt check, vet, tests · plugin: vitest, eslint, prettier check, typecheck+build
```

## 2. Pick the version(s)

Current plugin version: `manifest.json` `version`. Last server version:
`git tag -l 'server-v*' | sort -V | tail -1`. Review what's shipping per
artifact:

```sh
last=$(git tag -l '[0-9]*' | sort -V | tail -1)           # plugin
git log ${last:+$last..}HEAD --oneline -- plugin manifest.json versions.json
last=$(git tag -l 'server-v*' | sort -V | tail -1)        # server
git log ${last:+$last..}HEAD --oneline -- server
```

Pre-1.0 conventions: **patch** for fixes and internal-only changes, **minor**
for features, new settings, or anything behavior-breaking (frontmatter
contract changes, new required env vars, changed endpoints, template-contract
changes that themes target). The two artifacts version independently — don't
bump one because the other shipped. Propose version(s) with one-line
reasoning; if the changes argue for either, ask the user.

## 3. Bump the plugin version (plugin releases only)

```sh
node scripts/bump-plugin-version.mjs X.Y.Z
grep '"version"' manifest.json plugin/package.json   # must agree
node -p "require('./versions.json')['X.Y.Z']"        # must print the minAppVersion
```

If `minAppVersion` needs raising (new Obsidian API usage), edit
`manifest.json` first, then run the bump script.

## 4. Write the changelog

Update `CHANGELOG.md` (create it with a `# Changelog` header if missing),
inserting a new section at the top. The header format is load-bearing — the
release workflow matches it against the tag:

```markdown
## X.Y.Z (YYYY-MM-DD)

### Features
- …

### Fixes
- …
```

Write user-facing prose from the actual changes (read the diffs when commit
subjects aren't enough) — not raw commit subjects. Omit empty sections. Never
add an `### Internal` section or internal-only bullets (lint, typing,
refactors, tooling, review-bot cleanups) — the changelog is user-facing only;
that context belongs in the commit message. Call out anything self-hosters
must act on — new env vars, a server
restart needed for template changes, a "run Update all shared notes" needed
after theme-affecting changes — in an `### Upgrade notes` section. When a
server release ships alongside, describe its changes in a `### Server`
subsection of the same entry (server tags don't get their own release page).
For the first release (no prior tag), write release highlights rather than a
history.

## 5. Commit, tag, confirm, push

```sh
git add -A
git commit -m "Release plugin X.Y.Z"        # or "Release server X.Y.Z", or both
git tag -a "X.Y.Z" -m "Outcrop plugin X.Y.Z"            # plugin
git tag -a "server-vX.Y.Z" -m "Outcrop server X.Y.Z"    # server
```

No AI/tool attribution anywhere — commit, tag, or changelog.

**Confirm with the user before pushing** — show the version(s) and the
changelog section, and note that pushing publishes the GitHub release (which
BRAT users install immediately) and/or the public GHCR image. Then:

```sh
git push origin main "X.Y.Z" "server-vX.Y.Z"   # only the tags being cut
```

## 6. Watch the workflows and verify

```sh
gh run list --limit 3
gh run watch <run-id> --exit-status            # once per triggered workflow
gh release view "X.Y.Z"                        # assets: manifest.json, main.js, styles.css
```

Verify the artifacts are actually consumable:

```sh
# BRAT reads the release assets directly — manifest must carry the new version:
gh release download "X.Y.Z" -p manifest.json -O - | grep '"version"'
docker manifest inspect ghcr.io/sanketsaurav/outcrop:X.Y.Z | grep -c architecture   # ≥ 2
```

If the release notes came out as a commit list, the changelog header didn't
match `## X.Y.Z ` (trailing space, no `v` prefix). Fix `CHANGELOG.md` on main
and re-sync just the notes — don't re-tag:

```sh
awk -v v="## X.Y.Z " 'index($0, v) == 1 {f=1; next} /^## /{f=0} f' CHANGELOG.md > /tmp/notes.md
gh release edit "X.Y.Z" --notes-file /tmp/notes.md
```

Report the release URL when done.

## Failure modes

- **Release workflow fails on the tag/manifest check**: the tag doesn't match
  `manifest.json` — nothing was published. Fix the version on main (or the
  tag), then `git tag -d X.Y.Z && git push --delete origin X.Y.Z`, re-tag,
  re-push.
- **Workflow failed before anything was published**: same recovery — fix on
  main, delete and re-push the tag.
- **Release exists but is broken, and it's been public for more than a few
  minutes**: BRAT installs from latest release, so assume someone has it —
  fix forward with a patch release rather than re-cutting the same version.
- **Docker workflow failed**: re-run the failed run; it's idempotent (rebuilds
  and retags the same commit). A plugin release is unaffected and vice versa.
- **First GHCR push: image pulls fail anonymously**: new GHCR packages
  default to private. The user must set the `outcrop` package to public
  (GitHub → Packages → outcrop → settings) — one-time.
- **First plugin release**: versions are already correct in the repo — skip
  the bump and just tag. Submitting to the Obsidian community directory
  (community.obsidian.md, requires the owner's Obsidian account) is a
  separate, one-time process documented in CONTRIBUTING.md.

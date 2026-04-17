---
name: medusa-commits-and-releases
description: Use when writing a commit message in this repo, cutting a new medusa version, pushing a release tag, or creating a GitHub release. Codifies the conventional-commit prefixes that .goreleaser.yml's changelog filters rely on and walks through the make release flow end-to-end.
---

# medusa commits and releases

## Commit format

Commit subjects follow a conventional-commit-lite convention. The `.goreleaser.yml` changelog filters depend on these prefixes to keep release notes focused on user-facing changes.

**Subject line:** `<prefix>: <imperative summary>` — ≤72 chars, lowercase after the prefix.

**Allowed prefixes:**

| Prefix      | Meaning                                  | Surfaces in release notes? |
|-------------|------------------------------------------|----------------------------|
| `feat:`     | New user-facing feature                  | Yes                        |
| `fix:`      | Bug fix                                  | Yes                        |
| `refactor:` | Code restructuring, no behavior change   | Yes                        |
| `perf:`     | Performance improvement                  | Yes                        |
| `docs:`     | Documentation only                       | No                         |
| `test:`     | Tests only                               | No                         |
| `ci:`       | CI/workflow/tooling                      | No                         |
| `chore:`    | Routine maintenance, deps, housekeeping  | No                         |

**Optional PR reference** at end of subject: `(#123)`. GoReleaser carries it through to release notes; useful for linking context.

**Body** is optional. Include one when the *why* isn't obvious from the diff (hidden constraint, subtle invariant, reason for an unusual approach). Don't restate the *what* — the diff shows that.

### Examples

```
fix: correct typo in copy gitignored files checkbox label
feat: add restart option to close tab dialog (#42)
refactor: extract PTY catch-up flush into dedicated helper
chore: bump tmux minimum version note
```

### Anti-examples

```
Update stuff                              # no prefix, vague subject
feat: Added A Thing.                      # capitalized, period, past tense
Fix: bug in thing                         # wrong case, no detail
feat: massive overhaul of the state
machine plus some drive-by cleanups       # multi-purpose commit, unclear scope
```

## Release process

Releases are cut manually from a clean main checkout. There is no auto-release — nothing triggers on merge to main.

### Pre-flight checklist

1. On `main`, working tree clean: `git status` shows nothing.
2. Up to date with remote: `git pull --ff-only`.
3. Lint clean: `make lint`.
4. Tests pass: `make test`.
5. Harness smoke passes: `make release-check` (this is what the Makefile runs before tagging, but running it yourself first lets you catch issues without a dangling tag).
6. Optional dry-run: `GOCACHE=/tmp/gocache goreleaser release --snapshot --clean` and inspect `dist/` — 4 archives + `checksums.txt`.

### Pick the version

Semver: `vMAJOR.MINOR.PATCH`.

- Bug fixes only → bump `PATCH`.
- New features (backwards-compatible) → bump `MINOR`, reset `PATCH` to 0.
- Breaking changes → bump `MAJOR`, reset `MINOR` and `PATCH` to 0.

While medusa is pre-1.0, breaking changes may land under a `MINOR` bump — note them clearly in the commit.

### Tag and push

One command does everything:

```
make release VERSION=vX.Y.Z
```

That runs `release-check` → `release-tag` (annotated local tag) → `release-push` (`git push origin vX.Y.Z`).

### What happens next

Pushing the tag triggers `.github/workflows/release.yml`, which runs GoReleaser. GoReleaser:

1. Builds `medusa` and `medusa-approve-compound` for darwin/linux × amd64/arm64.
2. Packages each as a tar.gz archive with `LICENSE` + `README.md`.
3. Writes `checksums.txt`.
4. Generates release notes from git commits between the previous tag and this one, filtered by `.goreleaser.yml`'s `changelog.filters.exclude`.
5. Publishes a GitHub release at `https://github.com/Skowt/medusa/releases/tag/vX.Y.Z`.

### Post-release verification

- Release page lists 4 tar.gz archives + `checksums.txt`.
- Install-from-scratch smoke test into a throwaway dir:
  ```
  mkdir -p /tmp/medusa-smoke && INSTALL_DIR=/tmp/medusa-smoke \
    sh -c 'curl -fsSL https://raw.githubusercontent.com/Skowt/medusa/main/install.sh | sh'
  /tmp/medusa-smoke/medusa --version
  ```
- `medusa --version` reports the new tag.

## Troubleshooting

### Tag already exists, but CI didn't publish

Don't re-tag. Go to GitHub → Actions → Release workflow → "Run workflow" → enter the existing tag. The `workflow_dispatch` input re-runs GoReleaser against the existing tag without needing a new commit or tag.

### "Working tree not clean" from `release-tag`

The Makefile refuses to tag a dirty tree. Commit or stash, then retry `make release VERSION=vX.Y.Z`.

### GoReleaser fails mid-build

Inspect the CI log. Most transient failures are GitHub API timeouts during asset upload — re-run via `workflow_dispatch`. For reproducible failures (e.g. a build error because of a missing import), fix on a new commit, then choose whether to re-tag with the same version (move the tag: risky) or bump to the next patch.

### First-release changelog is huge

`v0.0.1` has no previous tag to diff against, so GoReleaser includes every non-filtered commit in history. Expected. Subsequent releases diff against the prior tag and are clean.

### Prefix visible in release notes

`.goreleaser.yml` uses `changelog.use: git` to produce a flat commit list. The conventional-commit prefix (`feat:`, `fix:`) appears in each rendered line. This is intentional — stripping prefixes in release notes would require a post-goreleaser rewrite hook. If you want to revisit, that's a polish task, not a blocker.

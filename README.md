<p align="center">
  <img width="339" src=".github/assets/medusa-logo.png" alt="Medusa" />
</p>

<p align="center">TUI for easily running parallel coding agents</p>

<p align="center">
  <a href="https://github.com/Skowt/medusa/releases">
    <img src="https://img.shields.io/github/v/release/Skowt/medusa?style=flat-square" alt="Latest release" />
  </a>
  <a href="LICENSE">
    <img src="https://img.shields.io/github/license/Skowt/medusa?style=flat-square" alt="License" />
  </a>
  <img src="https://img.shields.io/badge/Go-1.24.2-00ADD8?style=flat-square&logo=go&logoColor=white" alt="Go version" />
  <a href="https://discord.gg/Dswc7KFPxs">
    <img src="https://img.shields.io/badge/Discord-5865F2?style=flat-square&logo=discord&logoColor=white" alt="Discord" />
  </a>
</p>

<p align="center">
  <a href="#quick-start">Quick start</a> ·
  <a href="#how-it-works">How it works</a> ·
  <a href="#features">Features</a> ·
  <a href="#configuration">Configuration</a> ·
  <a href="#development">Development</a> ·
  <a href="#credits">Credits</a>
</p>

## What is Medusa?

Medusa is a terminal UI for running multiple coding agents in parallel with a worktree-first model that can import git worktrees.

## Prerequisites

- [tmux](https://github.com/tmux/tmux) 3.2 or newer — each agent runs in its own tmux session for terminal isolation and persistence.
- macOS or Linux. Windows is not supported.
- Go 1.24 or newer — only if building from source.

## Quick start

Install with the shell script:

```bash
curl -fsSL https://raw.githubusercontent.com/Skowt/medusa/main/install.sh | sh
```

Then run it:

```bash
medusa
```

Verify the install:

```bash
medusa --version
```

## Updating

Medusa checks for updates in the background on startup. When one is available you'll see a toast notification; open the Settings dialog to install it in place.

To update manually, re-run the install script:

```bash
curl -fsSL https://raw.githubusercontent.com/Skowt/medusa/main/install.sh | sh
```

## How it works

Each worktree tracks a repo checkout and its metadata. For local workflows, worktrees are typically backed by git worktrees on their own branches so agents work in isolation and you can merge changes back when done.

## Features

- **Parallel agents**: Launch multiple agents within main repo and within worktrees
- **No wrappers**: Works with Claude Code
- **Keyboard + mouse**: Can be operated with just the keyboard or with a mouse
- **All-in-one tool**: Run agents, view diffs, and access terminal

## Configuration

Create `.medusa/workspaces.json` in your project to customize how Medusa provisions workspaces:

```json
{
  "setup-workspace": [
    "npm install",
    "cp $ROOT_WORKSPACE_PATH/.env.local .env.local"
  ],
  "run": "npm run dev",
  "archive": "rm -rf node_modules"
}
```

- `setup-workspace` — shell commands run sequentially after a new workspace is created. Failures abort the workspace setup.
- `run` — the command invoked when you start the workspace's run script.
- `archive` — the command invoked when you archive the workspace.

Each command runs from the workspace's primary worktree root. Environment variables provided to every command include `$ROOT_WORKSPACE_PATH` (the project's root repo checkout) and an auto-allocated free port (see `internal/process/env.go`).

Workspace metadata is tracked in `~/.medusa/workspaces.json`.

## Development

Build and run from source:

```bash
git clone https://github.com/Skowt/medusa.git
cd medusa
make run
```

Or install directly with Go:

```bash
go install github.com/Skowt/medusa/cmd/medusa@latest
```

### Cutting a release

Releases are cut manually from a clean `main` checkout. A release is only triggered by pushing a `v*`-prefixed tag — nothing auto-releases on merge.

```bash
make release VERSION=vX.Y.Z
```

That target runs `release-check` (tests + harness smoke), `release-tag` (annotated local tag), and `release-push` (`git push origin vX.Y.Z`). The pushed tag triggers `.github/workflows/release.yml`, which runs GoReleaser and publishes darwin/linux × amd64/arm64 archives plus a `checksums.txt` to the GitHub releases page.

If the workflow run fails for a transient reason, re-trigger it without re-tagging: GitHub → Actions → Release → **Run workflow**, and enter the existing tag. Bump the patch version only if you need to ship different code.

The full commit conventions and release walkthrough live in `.claude/skills/medusa-commits-and-releases/SKILL.md`.

## Credits

Medusa was heavily inspired by [amux](https://github.com/andyrewlee/amux) by [@andyrewlee](https://github.com/andyrewlee). Thank you for the original design and the code Medusa builds on.

## Uninstalling

```bash
rm /usr/local/bin/medusa
```

State is kept under `~/.medusa/` (workspace registry, logs, workspace metadata). Remove it too if you want a clean slate.

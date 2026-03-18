<h1 align="center">Medusa</h1>

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
  <a href="#features">Features</a>
</p>

## What is Medusa?

Medusa is a terminal UI for running multiple coding agents in parallel with a worktree-first model that can import git worktrees.

## Prerequisites

Medusa requires [tmux](https://github.com/tmux/tmux). Each agent runs in its own tmux session for terminal isolation and persistence.

## Quick start

```bash
git clone https://github.com/Skowt/medusa.git
cd medusa
make run
```

## How it works

Each worktree tracks a repo checkout and its metadata. For local workflows, worktrees are typically backed by git worktrees on their own branches so agents work in isolation and you can merge changes back when done.

## Features

- **Parallel agents**: Launch multiple agents within main repo and within worktrees
- **No wrappers**: Works with Claude Code
- **Keyboard + mouse**: Can be operated with just the keyboard or with a mouse
- **All-in-one tool**: Run agents, view diffs, and access terminal

## Development

```bash
git clone https://github.com/Skowt/medusa.git
cd medusa
make run
```

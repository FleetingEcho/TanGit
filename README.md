<p align="center">
  <img src="docs/icon.png" width="88" alt="TanGit icon">
</p>

<h1 align="center">TanGit</h1>

<p align="center"><strong>Precise commits. Visual history. A native Git workspace.</strong></p>
<p align="center">Built with Go and MyGo · macOS · Windows · Linux</p>

<p align="center">
  <a href="https://github.com/FleetingEcho/TanGit/releases">Download</a> ·
  <a href="#features">Features</a> ·
  <a href="#get-started">Get started</a> ·
  <a href="#keyboard-shortcuts">Shortcuts</a> ·
  <a href="#architecture">Architecture</a> ·
  <a href="README-CN.md">简体中文</a>
</p>

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/screenshots/workspace.png">
    <source media="(prefers-color-scheme: light)" srcset="docs/screenshots/workspace_light.png">
    <img src="docs/screenshots/workspace.png" alt="TanGit workspace with repository navigation, changes, and a file diff">
  </picture>
</p>

<details>
<summary><strong>Screenshot gallery · 6 views</strong></summary>

<table>
  <tr>
    <td width="50%"><a href="docs/screenshots/workspace.png"><img src="docs/screenshots/workspace.png" alt="Workspace · Dark"></a><br><sub>Workspace · Dark</sub></td>
    <td width="50%"><a href="docs/screenshots/workspace_light.png"><img src="docs/screenshots/workspace_light.png" alt="Workspace · Light"></a><br><sub>Workspace · Light</sub></td>
  </tr>
  <tr>
    <td width="50%"><a href="docs/screenshots/graph_dark.png"><img src="docs/screenshots/graph_dark.png" alt="Commit graph · Dark"></a><br><sub>Commit graph · Dark</sub></td>
    <td width="50%"><a href="docs/screenshots/graph_light.png"><img src="docs/screenshots/graph_light.png" alt="Commit graph · Light"></a><br><sub>Commit graph · Light</sub></td>
  </tr>
  <tr>
    <td width="50%"><a href="docs/screenshots/picker.png"><img src="docs/screenshots/picker.png" alt="Repository picker"></a><br><sub>Repository picker</sub></td>
    <td width="50%"><a href="docs/screenshots/file-history.png"><img src="docs/screenshots/file-history.png" alt="File history"></a><br><sub>File history</sub></td>
  </tr>
</table>
</details>

## What is TanGit?

TanGit brings Lazygit's Git engine into a native desktop interface, with
keyboard navigation and visual tools for the work that needs a closer look.

- **Turn mixed edits into clean commits.** Stage, unstage, or stash selected lines across separate hunks, keeping the rest of your changes untouched.
- **Shape history visually.** Follow the commit graph and file history, then batch cherry-pick, squash, fix up, drop, or reorder commits.
- **Resolve conflicts in place.** Compare base, current, and incoming versions; choose individual hunks, edit the result, and undo a choice before staging.
- **Keep your development tools together.** Switch repository tabs, run a shell in the built-in terminal, and open your configured editor with one click.
- **Make the workspace yours.** Resize and expand panels, browse directory trees, and choose from 20 palettes, custom fonts, and six interface languages.

Confirmations explain consequential actions. Force push with lease, eligible
undo/redo operations, and reflog recovery help you manage changes deliberately.

## Features

| | What you can do |
| --- | --- |
| **Changes & diffs** | Stage files or selected lines; review unified, side-by-side, image, and media diffs; export patches. |
| **History & graph** | Filter commits, inspect file history, cherry-pick batches, reword, squash, and reorder commits. |
| **Branches & recovery** | Manage remotes, tags, stashes, worktrees, and submodules; inspect the reflog and undo eligible operations. |
| **Conflict resolution** | Review base, current, and incoming versions; choose hunks, edit the result, and stage the resolution. |
| **Your workspace** | Repository tabs, directory trees, resizable panels, expanded branch/diff views, a terminal, and external editors. |
| **Your preferences** | Themes, fonts, six interface languages, configurable visible sections, and signed in-app updates. |

Commit actions live in detail panels and right-click menus. Click a diff line
number to start multiple-line selection; Shift-click selects a range.

[Common workflows →](docs/guide.md#common-workflows)

## Get started

1. Install [Git](https://git-scm.com/) and download a [TanGit release](https://github.com/FleetingEcho/TanGit/releases).
2. Use the **DMG** on macOS, the **setup executable** on Windows, or the matching **`.deb` / archive** on Linux.
3. Open a repository, review its changes, stage what you need, and commit.

**From the terminal**, after [setting up the command for your platform](docs/guide.md#open-a-repository-from-the-terminal):

```sh
tangit .
tangit "./repository with spaces"
```

The macOS launcher and Windows PATH need one-time setup. The guide includes
instructions for existing installations and custom app locations.

**From your editor workflow:** choose a preset or command in **Settings → Editor**.
Editor buttons launch it directly. Neovim opens in a terminal; on macOS,
quitting Neovim returns to the shell in the project or file's directory.

## Keyboard shortcuts

| Keys | Action |
| --- | --- |
| `?` / `F1` | Current-context help / complete shortcut reference |
| `Tab` / `Shift+Tab`, `F6` / `Shift+F6` | Move between controls and panels |
| Arrows, `j` / `k` | Navigate the focused list |
| `Space` | Stage or unstage in Changes, including selected diff lines |
| `c` / `Ctrl+Enter` | Compose / submit a commit |
| `p` / `P` | Pull / push |
| `r` | Reword a commit in History or Graph |
| `z` / `Z` | Undo / redo eligible operations |
| `Ctrl+Tab` / `Ctrl+Shift+Tab` | Switch repository tabs |
| ``Ctrl+` `` / `Ctrl+,` | Terminal / Settings |

Uppercase letters mean **Shift + letter**. Git shortcuts follow the focused
panel; text fields retain normal typing. Attention-requiring actions ask for
confirmation. [More shortcuts →](docs/guide.md#keyboard)

## Architecture

TanGit is a Go application. **MyGo** renders the native UI, and **Lazygit**
provides the Git command layer. Repository sessions keep Git operations and
repository data separate from the views.

Views schedule work and display refreshed repository snapshots. MyGo's
headless tester exercises clicks, typing, keyboard focus, and rendered layouts.

## Build & contribute

Requires Go **1.27.3** and Git. Dependencies are declared in `go.mod`.

```sh
git clone https://github.com/FleetingEcho/TanGit.git
cd TanGit
go tool mygo dev      # develop
go tool mygo build    # build for this platform
```

[Development & checks](docs/guide.md#development) ·
[Release builds & signed updates](docs/guide.md#publishing-signed-updates) ·
[Issues](https://github.com/FleetingEcho/TanGit/issues)

---

[MIT License](LICENSE) · Built with [MyGo](https://github.com/egoist/mygo) and
[Lazygit](https://github.com/jesseduffield/lazygit).

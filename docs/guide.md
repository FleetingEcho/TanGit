# TanGit guide

[English overview](../README.md) · [简体中文概览](../README-CN.md)

[Terminal setup](#open-a-repository-from-the-terminal) · [Workflows](#common-workflows) ·
[Shortcuts](#keyboard) · [Development](#development)

## Open a repository from the terminal

After setup, run `tangit .` or `tangit "path/to/repository"`.
Relative paths use your current directory. Git must be on `PATH`.

### macOS

Move `TanGit.app` to Applications, then install the bundled launcher once:

```sh
sudo mkdir -p /usr/local/bin
sudo install -m 755 /Applications/TanGit.app/Contents/Resources/tangit /usr/local/bin/tangit
command -v tangit
tangit .
```

The v1.0.2 DMG has no bundled launcher. From a source checkout, use
`sudo install -m 755 resources/darwin/tangit /usr/local/bin/tangit` instead.
For installation without administrator access, use `~/.local/bin` if it is on your `PATH`.

The launcher returns immediately and opens the repository in a new app window.
It finds TanGit in `/Applications` or `~/Applications`. For another location:

```sh
TANGIT_APP="/path/to/TanGit.app" tangit .
```

### Windows

Current installers add `%LOCALAPPDATA%\Programs\TanGit` to your user `PATH`.
Open a new terminal after installing:

```powershell
tangit .
tangit "C:\Projects\Repository with spaces"
```

If the command is missing, check `Get-Command tangit` in PowerShell or
`where tangit` in Command Prompt. You can always launch by its full path:

```powershell
& "$env:LOCALAPPDATA\Programs\TanGit\TanGit.exe" .
```

For older installers or extracted archives, add the folder containing
`TanGit.exe` to PATH once; adjust `$dir` for a custom location:

```powershell
$dir = Join-Path $env:LOCALAPPDATA 'Programs\TanGit'
$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if (($userPath -split ';') -notcontains $dir) {
    $newPath = if ($userPath) { "$userPath;$dir" } else { $dir }
    [Environment]::SetEnvironmentVariable('Path', $newPath, 'User')
}
if (($env:Path -split ';') -notcontains $dir) { $env:Path += ";$dir" }
Get-Command tangit
```

### Linux

Packages provide `tangit`; the standalone installer uses `~/.local/bin`.
If upgrading from v1.0.0 through the in-app updater, reinstall a newer package
once to install the launcher. System-managed packages should be updated with
their package installer.

## Common workflows

| Task | Steps |
| --- | --- |
| Clone and open a repository | On Home, choose **Clone remote repository…** or press `C`. Enter an HTTPS, SSH, or scp-like URL, optionally choose a branch, and select a native destination parent. **Clone and open** shows Git's real progress and opens the completed repository. Escape cancels Git without deleting the destination. Credential-bearing URLs are rejected; authentication uses your configured Git credential helpers or SSH agent. |
| Stage selected lines | Click diff line numbers, then **Stage**. Shift-click selects a range; Escape clears the selection. |
| Build a Custom Patch | In History or Graph, use `Ctrl+B` on a commit or historical file to add canonical whole-file patches. In its diff, select changed lines and press `Space`, or press `A` for the current hunk. Press `B` to review the basket, preview the combined patch, then export it, apply it atomically to the working tree without staging, or send it to another worktree. This never rewrites history. Different commits cannot contribute changes to the same old/new path; additions, deletions, renames, binary files, and mode-only changes require whole-file selection. |
| Copy changes to another worktree | Open a changed file's menu and choose **Send file to worktree…**, use the diff's **Send current hunk** or **Send selected lines** action, or choose **Send to worktree…** in Custom Patch Builder. Change Courier freezes the selected patch, previews it against an existing destination worktree, and reports conflicts without writing. A clean delivery applies atomically as unstaged destination changes; the source and all commit history remain unchanged. To target an unchecked-out branch, create a worktree for it first. |
| Apply an external patch | In **Changes**, choose **Apply patch…** or press `Ctrl+I`, then select a `.patch` or `.diff` file. Review its files, diagnostics and text before applying. Clean patches apply atomically to the working tree and stay unstaged; conflicted patches cannot be applied. Cancel or Discard Patch changes neither the repository nor the source file. |
| Edit files in TanGit | Open **Editor**, select or quick-open a file, and edit its persistent tab. Use Ctrl/Cmd+S to save; a dot marks unsaved tabs. Closing asks whether to save or discard changes. |
| Manage editor tabs | Right-click a file tab to close it, other tabs, tabs to the right, or all tabs, or to copy its absolute or repository-relative path. |
| Manage files and folders | Use **Editor → New File / New Folder**, or right-click an explorer row to create, rename, delete, view Git file history, or copy paths. Creation starts in the selected folder; deleting asks for confirmation. |
| Move files and folders | Drag an explorer file or folder onto another folder. Open editor tabs and unsaved buffers follow renamed or moved files. |
| Use terminal tabs and maximize the dock | Open the status-bar terminal icon, then use **+** for an independent tab or the split buttons for panes within a tab. Tabs show their shell and working directory instead of generic numbers. Use the pencil or right-click menu to rename; the menu also closes the selected tab. Closing a tab asks for confirmation only when that tab contains multiple shells. An individual terminal’s **X** asks whenever any other terminal exists; the last terminal closes directly. Maximize fills the content area and hides redundant per-shell title bars; restore returns to the previous dock height. |
| Use an external editor | Set **Settings → Editor**, then click its editor button. Neovim opens in a terminal. |
| Edit several commits | Select rows with checkboxes or `Ctrl+Space`, then use **Batch actions** or the right-click menu. Rewrites require consecutive commits on the current branch. |
| Resolve conflicts | Choose or edit hunks, stage each resolved file, then continue the interrupted operation. |
| Recover work | Use Reflog to inspect earlier states and create a branch, check out, or reset after confirmation. In **Saved work → Safety Journal**, review discards, resets, rebases, branch deletions, dropped stashes, and patch applications. Successful operations offer immediate notification Undo and remain listed durably. Undo uses the exact operation's pinned object, saved work, or patch bytes and stops if HEAD or newer working-tree changes no longer match. Interrupted or failed entries stay visible without an unsafe Undo action. |
| Adjust the workspace | Drag dividers; use expand/shrink icons for full-window views. The folder button toggles directory grouping. |

All workspace sections are visible by default; choose which ones to show in **Settings → Workspace sections**. Use
**Settings → Updates** to check the installed version and available updates.
The Editor virtualizes large file trees, rebuilds them only when their contents or folding changes, and renders only the active editor tab.

## Keyboard

`?` shows context help; `F1` opens the complete, searchable shortcut reference.
Commands follow the focused panel. Uppercase letters mean **Shift + letter**.

| Context | Keys |
| --- | --- |
| Controls / panels | `Tab` / `Shift+Tab`; `F6` / `Shift+F6`; `Enter` activates; `Escape` returns |
| Lists / search | Arrows or `j` / `k`; `/` searches; `F3` / `Shift+F3` moves between matches; `Shift+F10` opens the row menu |
| Repository tabs | `Cmd/Ctrl+T` new, `Cmd/Ctrl+W` close; `Ctrl+Tab` / `Ctrl+Shift+Tab` switch |
| Home | `C` opens remote clone; `Escape` closes the form or cancels cloning |
| Global Git actions | `p` pull, `P` push, `z` / `Z` undo / redo, `F5` refresh |
| Changes | `Space` stage/unstage, `a` stage all, `c` compose, `A` amend, `s` stash, `d` discard, `b` blame, `Ctrl+I` import patch |
| Commit form | `Enter` submits the summary; `Ctrl+Enter` commits; `Escape` closes |
| Editor | `Ctrl+P` quick open; `Ctrl+Shift+F` searches workspace content; `Ctrl+W` closes the editor tab |
| History / Graph | `r` reword, `s` squash, `f` fixup, `d` drop, `t` revert; `Ctrl+J` / `Ctrl+K` reorder; `Ctrl+B` adds the selected commit/file to Custom Patch; `B` reviews it |
| Cherry-pick | `C` copies selected commits; `V` confirms applying them |
| Branches | `Space` checkout, `n` create, `M` merge, `r` rebase, `u` unset upstream, `d` delete |
| Stash / tags | Stash: `Space` apply, `g` pop, `d` drop. Tags: `n` create, `P` push, `d` delete |
| Diff review | `[` / `]` changes hunk; `v` starts line selection; `Shift+Up/Down` extends it; `Space` stages/unstages in Changes or adds selected lines to a historical Custom Patch; `A` adds the current historical hunk |
| Diff display | `W` compares refs; `Alt+W` toggles whitespace; `Alt+U` cycles context |
| Conflicts | `F7` / `Shift+F7` moves; `Alt+1`–`Alt+4` selects current/ancestor/incoming/both; `Alt+Z` undoes a choice |
| Terminal | ``Ctrl+` `` or `Ctrl+~` shows or hides the bottom panel from any workspace or form without changing sections; its tabs, splits, search and scrollback stay alive while hidden |
| Tools | `Ctrl+L` command log, `Ctrl+,` Settings |

## Development

Requires Go **1.27.3** and Git. Dependencies are downloaded from `go.mod`.
macOS builds require macOS 12 or newer; Linux packages use GTK 3 and AppIndicator.

```sh
git clone https://github.com/FleetingEcho/TanGit.git
cd TanGit
go tool mygo dev
```

Before submitting changes:

```sh
gofmt -l .
go vet ./...
go tool mygo vet .
CGO_ENABLED=0 go test ./...
```

Use `go test -race ./...` when changing shared state. Wrap new UI strings in
`tr(...)` and add translations to every catalog in `internal/app/i18n*.go`.

## Publishing signed updates

```sh
go tool mygo build -platform darwin/universal,windows/amd64,windows/arm64,linux/amd64,linux/arm64
```

macOS signing and DMGs need macOS; Windows installers need NSIS (`makensis`).
Build outputs are grouped by platform under `build/`.

1. Bump `mygo.json`'s version and commit the change.
2. Push the matching version tag to trigger the release workflow.
3. Check installers, signed archives, deltas, and all platform `update-*.json` manifests before publishing the draft.

The workflow needs the `MYGO_UPDATER_PRIVATE_KEY` repository secret. Keep the
same signing key so existing installations accept new updates. In-app updates
require write access to the installation folder.

<p align="center">
  <img src="docs/icon.png" width="104" alt="TanGit icon">
</p>

<h1 align="center">TanGit</h1>

<p align="center">
  A native Git desktop client, built in Go with
  <a href="https://github.com/egoist/mygo">MyGo</a> and
  <a href="https://github.com/jesseduffield/lazygit">Lazygit</a>'s Git engine.
</p>

<p align="center">
  <a href="#features">Features</a> ·
  <a href="#build-and-run">Build &amp; run</a> ·
  <a href="#keyboard">Keyboard</a> ·
  <a href="#how-it-works">How it works</a> ·
  <a href="#license">License</a>
</p>

![TanGit workspace](docs/screenshots/workspace.png)

TanGit brings Lazygit's fast, keyboard-driven Git workflow to a real desktop
window. The Git work — staging lines, cherry-picking, rebasing, reading the
reflog — runs through the same commands and models as Lazygit. The interface is
drawn natively by MyGo: no HTML, no JavaScript, and no bundled browser.

It is a small Go application with a bundled VT library, a terminal pane, and
every action reachable from both the keyboard and the mouse.

## Features

### Changes and commits

- Stage and unstage whole files, or **select lines inside a diff** to stage
  only part of a file.
- The working tree keeps itself current: changes, commits, and branches made
  in another app appear within seconds without pressing `F5`, and the file or
  commit you were reading stays chosen.
- Confirmation modals explain the target and consequences before publishing,
  pulling, switching branches, applying or saving stashes, changing tracking,
  updating submodules, resolving entire conflict files, or rewriting history.
- Stage, unstage, or discard everything at once, with a confirmation before
  anything is thrown away.
- Switch Changes, commit, branch-commit, and stash file lists between flat
  paths and sorted, collapsible directory trees. Directory folding never
  changes which source file a diff or row action targets.
- Commit with a one-line subject, expand it into a description, or amend the
  last commit.
- Stash the working tree or only the staged files, then apply, pop, or drop
  stashes.
- Resolve conflicts in place: navigate with `F7` / `Shift+F7`, keep the
  current, ancestor, incoming, or both hunks with `Alt+1` through `Alt+4`, undo
  a pick with `Alt+Z`, edit the file, then stage it. Continue stays disabled
  until every conflict is resolved.
- Unified or side-by-side diffs, wrapped long lines, fullscreen review, and
  **Save diff** to export a patch. Every ordinary diff can hide whitespace-only
  changes (`Alt+W`) and cycle its context (`Alt+U`); line staging automatically
  uses the canonical whitespace-sensitive, three-line patch.
- Add an untracked file to `.gitignore` with `I`, or to this clone's
  `.git/info/exclude` with `Shift+I`, from either the keyboard or its menus.
- Compare any two branches, tags, or commits with `W` or **Compare refs**.
  Review per-file diffs, swap the refs, ignore whitespace, and choose how much
  context to show without changing the working tree. From a commit, `H`
  immediately compares it with HEAD and `Shift+H` compares its first parent
  with the commit.
- A resizable **file history** drawer with three columns: the file's commits,
  the files each commit changed, and the per-file diff. Restore a historical
  file from History, Graph, branch commits, or this drawer with `Shift+R` or its
  context menu; confirmation replaces only the working-tree file, never the
  index.
- Blame the selected file, and copy a path or commit hash from its row.

### History, graph, and filtering

- Browse commits with their author, age, branches, and tags, paging older
  history as you scroll. **History and Graph start on the current branch**;
  clear the branch filter to browse all refs.
- **Filter commits** by message, author, and branch, with a match count.
  While a filter is active, one small **×** clears every field at once.
- The Graph draws the filtered history as branch lanes, newest first, in the
  same list you already use.
- Per-commit actions: copy hash, soft, mixed, or hard reset, create a tag or
  branch there, cherry-pick onto the current branch, or revert.
- Select multiple commits in History or Graph using their checkboxes or `Ctrl+Space`.
  **Batch actions** and the selected rows’ context menus offer cherry-pick,
  squash, fixup, drop, and moving the selection toward newer or older history.
  Rewrites require consecutive commits on the current branch; squash and fixup
  retain the oldest selected commit.
- **Push options → Force push with lease** publishes rewritten branch history
  after confirmation. The current branch’s context menu offers the same action.
  Git refuses the push if the remote differs from its remote-tracking reference.
- Edit history without leaving the app: reword, drop, reorder, squash, fix up,
  amend older commits, and create or apply fixup commits. Git's todo-file editor
  handoff runs inside the TanGit executable rather than a terminal editor.

### Branches, remotes, and the rest

- Branches: check out, create, rename, merge into the current branch, rebase
  onto, pull, unset an upstream, and delete — with a force-checkout confirmation
  when work would be lost.
- Remotes: add, edit, fetch, and remove remotes; check out a remote branch as a
  local one or as detached HEAD, merge or rebase onto it, set it as the current
  branch's upstream, copy its full name, or delete it on the remote after
  confirmation.
- Tags: create, push, and delete.
- Stash, worktrees, submodules, and reflog each have their own workspace
  section.
- Press `/` to search files, branches, remotes and their branches, stashes,
  tags, worktrees, submodules, or reflog entries, with match counts and a clear
  action. `F3` / `Shift+F3` and the visible chevrons move to the next / previous
  match, wrapping at either end; History and Graph use their structured commit
  filter.
- Undo and redo eligible commits, resets, pulls, checkouts, and completed
  rebases through persistent reflog markers, with auto-stash protection.
- Recover manually from the reflog by checking out a commit, creating a branch
  there, or resetting the current branch with soft, mixed, or hard semantics.
- The sidebar shows the five everyday sections by default. Turn the rest on in
  **Settings → Workspace sections**.

### The workspace

- Open several repositories in **tabs** (`Ctrl+T`, `Ctrl+W`, `Ctrl+Tab`), with
  a picker that remembers recent repositories.
- Open a repository directly from a terminal, including relative paths and
  paths containing spaces. Linux packages provide **`tangit <path>`**; macOS
  and Windows launcher setup is covered below.
- A **tabbed title bar** that drags the window: the system's traffic lights on
  the left on macOS, and minimize, full screen, and close drawn on the right on
  Linux and Windows.
- A **terminal pane** running your shell in the repository, toggled with
  ``Ctrl+` ``; it keeps running while hidden.
- A **command log** recording every Git command and the output it printed.
- Close the window and TanGit stays in the menu bar or notification area:
  click its icon to reopen the window or quit for good.
- Open the repository or a file in your editor: presets for VS Code, Zed, and
  Neovim, or a custom command with `{{filename}}` and `{{line}}` placeholders.
- Toasts for actions and failures; a failed UI frame falls back to a safe
  screen instead of taking the window down.

### Make it yours

- **20 palettes plus System**: Tokyo Night (the default), Dracula, Gruvbox,
  GitHub, Nord, One, Solarized, all four Catppuccin flavors, Rosé Pine,
  Kanagawa Wave, Everforest Dark, and Ayu Mirage, with light variants where
  available.
- Accent colors (17 muted tones plus the palette's own), font family and size,
  spacing, and corner radius.
- English (default), Simplified Chinese, Traditional Chinese, Japanese,
  Spanish, and French.
- No telemetry. Installed copies check signed GitHub releases in the background
  after every launch; when a newer version is available, the title bar's update
  icon lights up with a **red badge** and opens the release notes and install
  flow. **Settings → Updates** also shows the running version and links to the
  GitHub releases page.

### Screenshots

TanGit browsing the [Lazygit](https://github.com/jesseduffield/lazygit)
repository. The image follows your GitHub color scheme:

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/screenshots/graph_dark.png">
    <source media="(prefers-color-scheme: light)" srcset="docs/screenshots/graph_light.png">
    <img src="docs/screenshots/graph_light.png" alt="TanGit showing the Lazygit repository's commit graph and a side-by-side diff">
  </picture>
</p>

More of the workspace:

<p align="center">
  <img src="docs/screenshots/picker.png" width="49%" alt="Repository picker">
  <img src="docs/screenshots/file-history.png" width="49%" alt="File history">
</p>

## Build and run

Prebuilt packages are attached to
[GitHub releases](https://github.com/FleetingEcho/TanGit/releases) when
published. The steps below run and build TanGit from source.

### Requirements

- Linux (X11 or Wayland), macOS 11 or newer (Apple silicon and Intel), or
  Windows 10/11.
- Git available on `PATH`.
- To build: Go **1.27.3**, as pinned by `go.mod`.
- On Linux the package depends on GTK 3 and `libayatana-appindicator3-1`,
  which `apt` installs with the `.deb`. Without AppIndicator, TanGit still
  runs, and closing the window quits as before.

> **Building from source:** everything TanGit needs is declared in `go.mod`,
> which points at the upstream Lazygit module. The Go toolchain downloads the
> dependency; no sibling checkout is required. The few additions TanGit needs
> from Lazygit's command layer live in `internal/repository/lazygit_ext.go`,
> built on the module's public API, so a fresh clone builds on its own.

```sh
git clone https://github.com/FleetingEcho/TanGit.git
cd TanGit

# Run the app in development mode.
go tool mygo dev

# Build for the host platform.
go tool mygo build

# Build every release target for v1.0.1.
go tool mygo build -platform darwin/universal,windows/amd64,windows/arm64,linux/amd64,linux/arm64

# Install on Debian or Ubuntu.
sudo apt install ./build/linux-amd64/tangit_1.0.1_amd64.deb
```

### Open a repository from the terminal

TanGit accepts a repository path on every platform. Relative paths are resolved
from the directory where you run the command; quote paths containing spaces.

#### Linux

The v1.0.1 and later packages install **`tangit <path>`** on `PATH` (the
standalone installer uses `~/.local/bin`). An in-app update from v1.0.0 does
not add the newly introduced launcher, so reinstall a v1.0.1 or later package
once when upgrading that installation.

```sh
# Open the current repository from inside it.
tangit ./

# Open another repository, quoting paths that contain spaces.
tangit "./path with spaces/repo"
```

#### macOS

The DMG installs `TanGit.app` but does not install a shell command. To create
a `tangit` launcher on the standard `/usr/local/bin` path, run this once:

```sh
sudo mkdir -p /usr/local/bin
sudo ln -sf "/Applications/TanGit.app/Contents/MacOS/TanGit" /usr/local/bin/tangit
```

If the app is in your user Applications folder, replace `/Applications` in the
command with `"$HOME/Applications"`. Open a new terminal afterwards. The link
continues to work after in-place updates while the app stays in that folder.

#### Windows

The installer puts `TanGit.exe` in `%LOCALAPPDATA%\Programs\TanGit` but does not
add that folder to `PATH`. PowerShell can open the current repository without
changing `PATH`:

```powershell
& "$env:LOCALAPPDATA\Programs\TanGit\TanGit.exe" .
```

To enable `tangit <path>` in new PowerShell and Command Prompt windows, add
the install folder to your user `PATH`:

```powershell
$dir = Join-Path $env:LOCALAPPDATA 'Programs\TanGit'
$oldPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if (($oldPath -split ';') -notcontains $dir) {
    [Environment]::SetEnvironmentVariable('Path', "$oldPath;$dir", 'User')
}
```

For a custom installation folder, use that folder instead of the default above.

For development on any platform, this equivalent runs from the TanGit source
tree:

```sh
go run ./cmd/tangit .
```

### Release artifacts

Each target gets its own directory under `build/`:

```
build/
├── darwin-universal/
│   ├── TanGit.app                  # arm64 + Intel, one bundle
│   └── tangit-1.0.1.dmg            # on macOS, signed when configured
├── windows-amd64/  windows-arm64/
│   ├── TanGit.exe
│   ├── ghostty-vt.dll
│   └── tangit-setup-1.0.1-<arch>.exe   # when NSIS is installed
├── linux-amd64/    linux-arm64/
│   ├── tangit
│   ├── tangit_1.0.1_<arch>.deb
│   ├── tangit-1.0.1-linux-<arch>.tar.gz
│   ├── PKGBUILD                    # Arch package recipe
│   └── install.sh
└── ...
```

MyGo needs no cgo, so any target can be compiled from any machine. Two steps
still need their host tooling:

- **macOS disk images and signing** need macOS. Elsewhere the universal
  `.app` is built and the DMG step is skipped with a warning.
- **Windows installers** need NSIS (`makensis`) on `PATH`. Without it the
  `.exe` is built and the setup step is skipped; pass `-skip-installer` to
  skip it quietly.

### Publishing signed updates

The release workflow builds each platform, signs its update archive, and
uploads the installers and update manifests to a draft GitHub release. Before
using it for the first time, add the generated private key as a repository
secret (never commit this key):

```sh
gh secret set MYGO_UPDATER_PRIVATE_KEY \
  < ~/.config/mygo/update-keys/mygo-update.key
```

For each release, bump `version` in `mygo.json`, commit it, then push the
matching tag (for example, `v1.0.1`). After the workflow finishes, review and
publish its draft release. Installed copies will then discover the signed
update automatically.

## Keyboard

TanGit is keyboard-first. Press **?** for the shortcuts available on the
current page, or **F1** for the complete, filterable shortcut list; the header's
more menu opens the same full reference.
The switch in the shortcut reference and Settings enables or disables keyboard
shortcuts immediately and saves the choice. The keyboard icon in the bottom
status card opens the reference, even when shortcuts are disabled.

The repository path receives focus on launch. Tab also reaches each recent
repository's Open action. In workspace lists and read-only diffs, Tab moves to
the next panel rather than through every row’s controls. Git action shortcuts
follow the focused list or diff;
text fields and the terminal keep ordinary typing. Dangerous actions still ask
for confirmation, and dialogs return focus to the workspace when closed.
Uppercase letters below mean **Shift + letter**.

| Navigation | Keys |
| --- | --- |
| Toolbar and dialog controls | `Tab` / `Shift+Tab`; `Enter` or `Space` activates |
| Panels | `Tab` / `Shift+Tab` from lists and diffs; `F6` / `Shift+F6` also work; `Enter` moves into details; `Escape` returns |
| Lists | Arrows or `j` / `k`; `Home` / `End`; `Shift+F10` opens the selected row's menu |
| Sections | `1`–`9`, `0` switch visible workspace sections |
| Repository tabs | `Cmd/Ctrl+T`, `Cmd/Ctrl+W`, `Ctrl+Tab`, `Ctrl+Shift+Tab` |
| Help and search | `?` current context, `F1` complete reference, `/` search, `F3` / `Shift+F3` next / previous match |
| Preferences and tools | `Ctrl+,` settings, `Ctrl+L` command log, ``Ctrl+` `` terminal |

| Git operations | Keys |
| --- | --- |
| Global | `p` pull, `P` push, `z` / `Z` undo / redo; `Ctrl+Z` / `Ctrl+Shift+Z` remain aliases; `F5` refresh |
| Changes | `Space` stage/unstage, `a` stage all, `c` compose, `A` amend, `s` stash, `f` fetch, `i` / `I` ignore/exclude, `d` discard, `b` blame |
| Commit form | `Enter` submits the summary; `Ctrl+Enter` commits from either message field; `Escape` closes |
| History / Graph | `r` reword, `s` squash, `f` fixup, `F` create fixup, `S` apply fixups, `A` amend, `d` drop, `t` revert; `Ctrl+J` / `Ctrl+K` reorder |
| Cherry-pick | `C` copies selected commits; `V` confirms applying copied commits to the current branch |
| Historical files | `h` compare with HEAD, `H` compare parent with commit, `Shift+R` restore selected file |
| Branches | `Space` checkout, `n` create, `M` merge, `r` rebase onto, `u` unset upstream, `d` delete |
| Remotes | `n` add, `f` fetch, `e` edit, `d` remove; Enter focuses remote branches |
| Remote branches | `Space` checkout as local, `Shift+Space` checkout detached, `M` merge, `r` rebase, `u` upstream, `d` delete |
| Stash | `Space` apply, `g` pop, `d` drop |
| Tags | `n` create, `P` push, `d` delete |
| Worktrees | `n` create, `o` open, `d` remove |
| Submodules | `n` add, `u` update, `d` delete |
| Diff review | `[` / `]` previous / next hunk; `j` / `k` move; in Changes, `v` starts line selection, `Shift+Up/Down` or `j` / `k` extend it, `Space` stages/unstages selected lines |
| Diff display | `W` compare refs, `Alt+W` ignore whitespace, `Alt+U` cycle context |
| Conflicts | `F7` / `Shift+F7` navigate; `Alt+1`–`Alt+4` keep current/ancestor/incoming/both; `Alt+Z` undo a choice |

## How it works

| Path | Responsibility |
| --- | --- |
| `cmd/tangit` | Thin executable entry point |
| `internal/app/main.go`, `shell.go`, `view_shell.go` | Window, tabs, picker, and workspace chrome |
| `internal/app/view_*.go` | One file per section or pane (changes, history, graph, branches, …) |
| `internal/app/actions.go` | Git actions, confirmations, and background work |
| `internal/repository` | Lazygit's Git commands behind a small session interface |
| `internal/app/settings.go`, `i18n*.go`, `notifications.go` | Preferences, translations, and toasts |
| `internal/app/*_test.go` | Headless UI tests, including `screenshot_test.go` |

The `internal/repository` package wraps Lazygit's command layer and models in
a `Session` interface. The UI never shells out to Git itself: it schedules
actions, the session runs them, and the next frame reads a fresh snapshot.
That keeps reads and writes serialized and the panes consistent. Where the app
needs something the library does not expose, `lazygit_ext.go` builds it on the
library's public API rather than patching a copy of the library.

The interface is tested without a window. `mygo/ui` provides a headless tester
that clicks, types, and renders frames, so behaviour, layout, and screenshots
are all exercised by `go test`.

Settings and recent repositories live in MyGo's user-data directory, usually
`~/.config/TanGit` on Linux. They are written atomically and saved as you
change them.

## Development

```sh
gofmt -l .                 # formatting
go vet ./...               # static checks
go test ./...              # all tests
go test -race ./...        # race detector
go tool mygo build         # release build

# Regenerate the screenshots used above (and the rest of the gallery).
MYGO_SHOTS=/tmp/tangit-shots go test ./internal/app -run TestScreenshot
```

There is no separate lint configuration: `gofmt` and `go vet` are the
standard. Wrap new interface strings in `tr(...)` and add the same English key
to every catalog in `internal/app/i18n*.go`; the English catalog is the source
text itself.

## Contributing

Issues and pull requests are welcome. For a change to the interface, include a
screenshot from `MYGO_SHOTS` if you can; it makes a review much faster. Run the
full test suite and `go vet` before opening a pull request.

## License

TanGit is released under the [MIT License](LICENSE).

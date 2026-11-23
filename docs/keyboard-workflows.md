# Keyboard workflows and lazygit comparison

## Source of truth

The comparison was made against the pinned lazygit dependency's
`docs/keybindings/Keybindings_en.md`. lazygitGo keeps the same, context-scoped
operation definitions for registration, help text and command provenance; a
`MenuItem.Shortcut` is display metadata, not a complete replacement for an
application dispatch entry.

The Command Log records **accepted invocations**, not a claim that they
completed. Its immutable `ShortcutInvocation` contains an action ID, canonical
chord, display label and logger-assigned sequence. Subsequent Git commands and
completion output retain their own entry and output ownership. A newer input
must not be inferred from a mutable “last shortcut” variable inside an
asynchronous worker.

The modal Command Log presents the newest 20 matching records first and
virtualizes their rows. Real user scrolling to the end reveals another 20;
programmatic failure navigation instead finds the exact logger ID and expands
only enough of the page to reveal it. Status, recorded source and time-range
filters coexist with the existing origin tabs and command/output search.
Invocation provenance is not guessed from executable text. Shortcut and
palette rows describe accepted input; any process started subsequently keeps
its own running, failed or successful result and output in the same canonical
logger, rather than borrowing an earlier row's ID or status.

## lazygit-aligned additions

| Workspace | Key | Behavior |
| --- | --- | --- |
| History / Graph | `Shift+T` | Create a tag from the selected commit. |
| History / Graph | `n` | Create a branch from the selected commit. |
| Branches | `f` | Fast-forward the selected branch, with the normal confirmation and refresh path. |
| Branches | `Shift+F` | Force checkout, retaining destructive confirmation. |
| Branches | `Shift+R` | Open rename for the selected branch. |
| Stash | `r` | Open the stash rename form. |
| Worktrees | `Space` | Open the selected worktree. |
| Changes | `a` | Toggle Stage All / Unstage All according to the current index. Stage All runs only when nothing is staged. |

These operations share the existing repository action and confirmation paths.
Keyboard input is not another way to bypass conflict, destructive-action, or
operation-state safeguards. `A` no longer stages everything when the user has
explicitly asked to unstage an already staged index.

F3/Shift+F3 performs next/previous search navigation. F6/Shift+F6 and pane
navigation are scoped, rather than copied to unrelated editing contexts. Both
built-ins and user-defined shortcut commands use semantic action IDs configured
at registration time.

## Provenance and notifications

The **Behavior** section of Settings includes **Shortcut notifications**. It is
enabled by default and in older settings files because absence of the flag means
notifications are allowed. Turning it off suppresses only shortcut
acknowledgement toasts. Command Log entries and operation success, error and
recovery notices remain visible.

Each accepted invocation appears in the user's Command Log with its chord and
action, and appears in copied log text. The query searches the chord, action ID,
label and output. Clicking or typing ordinary text into a terminal or text input
does not masquerade as a lazygitGo command or reveal the typed content.

`AlreadyRecorded` distinguishes application callbacks which have already logged
an invocation from framework-only navigation. It is also available to callers
which have a manual invocation logger and want to avoid a duplicate native
notification without suppressing real events.

## Command Catalog and Palette invocation

`Ctrl/Cmd+Shift+P` opens the shell-owned Command Palette. The shared catalog
adapts the existing keyboard, hunk/conflict, navigation and custom-command
methods instead of introducing a second Git execution path. Candidates contain
no callback: `Invoke` resolves the stable command ID and availability again at
execution time, after selection, refresh or tab changes. Unsupported actions
remain searchable with a concrete disabled reason.

Accepted palette commands use `CommandLog.RecordPalette`, retaining the stable
command ID and `palette` provenance without a fabricated chord or shortcut
toast. Shortcut commands continue to use `ShortcutInvocation`; palette entries
are deliberately not relabeled as keyboard events. While a shell overlay is
open, ordinary app and window terminal shortcuts are not registered, and MyGo
restores the opener focus where it still exists.

## Native widgets and modal scopes

The vendored MyGo compatibility layer accepts a per-runtime
`Context.ObserveShortcuts` callback. With no observer, shortcut dispatch is
unchanged and no events are generated. When installed, matched element, window
and submit handlers produce value-only `ShortcutEvent` values. Descriptions and
annotations are scoped to the current pass and discarded on close.

The observer is called from the actual accepted dispatch path for
`node.Shortcut`, including the native `ListState.coreShortcut` path used for arrow
navigation, page movement and submit behavior. It is not called solely because a
view was built, which prevents construction-loop events. Pointer clicks are not
misreported as keyboard actions.

Application helpers call `Context.DescribeShortcut` or
`Element.DescribeShortcut` before binding. Root/window registrations use the
window receiver ID, while element registrations resolve the same owning node as
their actual action handler. IDs, translated labels and `AlreadyRecorded` are
never captured from an expired Element or a speculative “current shortcut” slot.

## Verification references

- `internal/app/shortcut_audit_test.go`: custom F5 provenance, native Down,
  no-input behavior, preference isolation, valid commit metadata and copy text.
- `internal/app/lazygit_shortcuts_test.go`: branch, history and stash workflows,
  including destructive confirmation and stage-all toggling.
- `internal/repository/shortcut_log_test.go`: immutable metadata and independent
  real-command output/error owners.
- `third_party/mygo/ui/shortcut_observer_test.go`: native selection once per
  accepted action, nil-observer defaults, no duplicate recorder and submit-only
  invocation.

Pointers into Element, node, runtime Context and list internals never enter the
log. It can outlive rendering during later worker callbacks only through string
and scalar values. The callback stays within the original window and is cleared
with that runtime.

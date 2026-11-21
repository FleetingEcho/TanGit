# Workflow entry and Command Log verification

This note records checks for the Command Palette, repository Overview, and the
revised Command Log based on `9eb8d79` plus the working-tree changes. There is
no standalone Activity Center modal or toolbar command. Overview's recent
activity opens the existing command, Journal, Agent, transfer or workspace
viewer by stable identity; it does not create another persistent event store.
This is not a release publication record.

## Code-level gates

Environment: Linux AMD64, Go `1.27.4`, with `TMPDIR` isolated under the Tools
work area. These checks completed successfully for the implementation:

| Check | Result |
| --- | --- |
| Focused workflow regression | `CGO_ENABLED=0 go test ./internal/app -run 'CommandLog\|CommandPalette\|CommandCatalog\|Cockpit\|Overview\|ActivityFeed\|ActivityProjection\|RemoteTransfer\|ToolbarVisibility\|TranslationSourceKeys\|Modal\|Shell' -count=1` passed (`7.144s`) |
| Standard static analysis | `go vet ./...` completed with no diagnostics |
| MyGo lifetime/state analysis | `go tool mygo vet .` completed with no diagnostics |
| Targeted race coverage | `go test -race ./internal/app -run 'CommandLog\|CommandPalette\|Cockpit\|Overview\|RemoteTransfer' -count=1 -timeout 15m` passed (`6.561s`) |
| Command Log screenshots | `MYGO_SHOTS=... CGO_ENABLED=0 go test ./internal/app -run '^TestCommandLogScreenshots$' -count=1`: all four scenarios passed (`0.391s`) |
| Narrow real geometry | `TestCommandLogNarrowGeometry` passed at 720 DIP with a long command, success badge and GitHub Light theme |
| Complete package suite | `CGO_ENABLED=0 go test ./... -count=1`: all packages passed (`internal/app` 86.428s, `internal/repository` 130.900s) |

Logs are under `tmp/tangit-workflow-plan`, principally
`command-log-regression.log`, `command-log-race.log`, and
`command-log-full-final.log`. PNGs are under `tmp/tangit-command-log-shots`.
These development logs and images are not release assets or native-GUI
acceptance evidence.

The visual review covered wide dark, 720-DIP light, failed-filter and
paginated dark states. It confirmed the green circular-chevron success icon,
**Succeeded** badge, compact time, source/provenance line, filter controls,
expandable failure output, and **Showing 40 of 55 commands** paging footer.
The narrow geometry check uses the actual text line, not the surrounding
virtual-list accessibility row, and verifies that its status does not overlap
or leave the dialog bounds.

## Behavioral evidence

The tests cover:

- all requested ranges: last hour, 3 hours, 24 hours, 3 days, 1 week, and all
  time, including boundary-excluded and zero-timestamp records;
- successful, failed and running status, exact row treatment, and output
  show/hide/copy controls;
- recorded background/direct, keyboard and Command Palette sources, including
  corresponding origin-tab selection and cleared filters;
- an initial page of exactly 20, another 20 after a real user wheel scroll,
  a final partial page, no page growth during ordinary repaints, and stable
  command identity when newer records arrive;
- exact failure-ID navigation that expands the page far enough to reveal an
  older record, plus real gated execution changing its exact row from
  **Running** to **Succeeded** while unrelated Git commands continue;
- keyboard and modal isolation, stable catalog invocation IDs, settings and
  repository start migration, translated source keys, and bounded Overview
  projections and activity navigation.

The pagination controls rendered records only. The canonical mutex-protected
Command Log continues recording into its existing bounded session store;
reaching the end does not run Git, read a new history database or perform I/O.
Copy log still includes complete timestamps and output for every filtered
match, including records outside the visible page and collapsed output.

## Remaining verification limits

These were **not** established in this environment:

- native overlay, focus restoration and IME behavior in a running GUI on Linux;
- any native GUI behavior on macOS or Windows;
- physical high-DPI and window-resize behavior, transparent rounded corners,
  or every dock/sidebar combination and text scale;
- native acceptance of all existing features alongside these workflow changes;
- release publication from this working tree.

`ui.Tester` images are headless rendering evidence only. Repeat overlay and
focus checks on actual Linux, macOS and Windows builds and attach those results
to the release verification record before publication.

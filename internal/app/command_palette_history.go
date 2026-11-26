package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

type paletteHistoryEntry struct {
	ID        uint64                         `json:"id"`
	Kind      string                         `json:"kind"`
	Text      string                         `json:"text"`
	Query     string                         `json:"query,omitempty"`
	Action    int                            `json:"action,omitempty"`
	Commit    *repository.Commit             `json:"commit,omitempty"`
	Command   commandID                      `json:"command,omitempty"`
	CodeQuery *repository.HistoryChangeQuery `json:"codeQuery,omitempty"`
	Workspace string                         `json:"workspace,omitempty"`
	At        time.Time                      `json:"at,omitempty"`
}

const (
	paletteHistorySearch  = "search"
	paletteHistoryCode    = "code"
	paletteHistoryCommand = "command"
	paletteHistoryShell   = "shell"
)

type paletteHistoryStore struct {
	Paused  bool                  `json:"paused"`
	Entries []paletteHistoryEntry `json:"entries"`
}

type paletteHistoryState struct {
	paletteHistoryStore
	loaded     bool
	loadFailed bool
	rewrite    bool
	path       string
	nextID     uint64
	open       bool
	clear      bool
	filter     string
	err        string
	lastQuery  string
	multi      bool
	focusList  bool
	cursor     int
	selected   map[uint64]bool
	list       ui.ListState
}

func (s *shell) loadPaletteHistory(a *app) {
	if a == nil {
		return
	}
	h := &s.palette.history
	dir := a.dataDir()
	if h.loaded {
		return
	}
	h.loaded = true
	if dir != "" {
		h.path = filepath.Join(dir, "command-palette-history.json")
		data, err := os.ReadFile(h.path)
		if err == nil {
			err = h.decode(data)
			if err == nil {
				return
			}
		}
		if !errors.Is(err, os.ErrNotExist) {
			h.loadFailed = true
			h.err = safeError(err)
			return
		}
	}
	h.rewrite = true
	// Older profiles saved recent commands and shell lines without timestamps.
	// Import each list in its original order.
	for _, list := range [][]string{a.settings.ShellHistory, a.settings.RecentCommands} {
		h.nextID += uint64(len(list))
	}
	id := h.nextID
	for _, line := range a.settings.ShellHistory {
		h.Entries = append(h.Entries, paletteHistoryEntry{ID: id, Kind: paletteHistoryShell, Text: line})
		id--
	}
	for _, command := range a.settings.RecentCommands {
		text := command
		for _, spec := range staticCommandSpecs() {
			if spec.ID == commandID(command) {
				text = tr(spec.Label)
				break
			}
		}
		h.Entries = append(h.Entries, paletteHistoryEntry{ID: id, Kind: paletteHistoryCommand, Text: text, Command: commandID(command)})
		id--
	}
	slices.Reverse(h.Entries)
}

// The first JSON record is a snapshot. Subsequent records append one entry
// each so a long history does not have to be rewritten on every search.
func (h *paletteHistoryState) decode(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&h.paletteHistoryStore); err != nil {
		return err
	}
	for _, entry := range h.Entries {
		h.nextID = max(h.nextID, entry.ID)
	}
	for {
		var entry paletteHistoryEntry
		err := decoder.Decode(&entry)
		switch {
		case errors.Is(err, io.EOF):
			return nil
		case errors.Is(err, io.ErrUnexpectedEOF):
			// A crash during an append can leave an incomplete last record.
			h.rewrite = true
			return nil
		case err != nil:
			return err
		}
		h.nextID = max(h.nextID, entry.ID)
		h.Entries = append(h.Entries, entry)
	}
}

func (h *paletteHistoryState) save() error {
	if h.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(h.path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(h.paletteHistoryStore)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(h.path), ".palette-history-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	_, writeErr := tmp.Write(append(data, '\n'))
	if err := errors.Join(writeErr, tmp.Close()); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), h.path); err != nil {
		return err
	}
	h.rewrite = false
	return nil
}

func (h *paletteHistoryState) appendEntry(entry paletteHistoryEntry) error {
	if h.path == "" {
		return nil
	}
	if h.rewrite {
		return h.save()
	}
	file, err := os.OpenFile(h.path, os.O_WRONLY|os.O_APPEND, 0)
	if errors.Is(err, os.ErrNotExist) {
		return h.save()
	}
	if err != nil {
		h.rewrite = true
		return err
	}
	err = errors.Join(json.NewEncoder(file).Encode(entry), file.Close())
	h.rewrite = err != nil
	return err
}

func (s *shell) recordPaletteHistory(a *app, entry paletteHistoryEntry) {
	s.loadPaletteHistory(a)
	h := &s.palette.history
	if h.Paused || h.loadFailed || entry.Text == "" || a == nil {
		return
	}
	h.nextID++
	entry.ID, entry.At = h.nextID, time.Now()
	entry.Workspace = paletteHistoryWorkspace(a)
	h.Entries = append(h.Entries, entry)
	h.err = safeError(h.appendEntry(entry))
}

func (s *shell) rememberPaletteQuery(a *app, query string) {
	query = strings.TrimSpace(query)
	mode, text := commandQueryMode(query)
	if mode == '!' || text == "" || query == s.palette.history.lastQuery {
		return
	}
	if code, ok := parseHistoryChangesPalette(text); mode == '#' && ok {
		// The submitted query is saved from the code-search panel.
		if code.Text != "" {
			s.palette.history.lastQuery = query
		}
		return
	}
	switch strings.ToLower(text) {
	case "by:", "author:", "branch:", "on:":
		return
	}
	s.palette.history.lastQuery = query
	s.recordPaletteHistory(a, paletteHistoryEntry{Kind: paletteHistorySearch, Text: query})
}

// Rebuild the older suggestion and shell recall lists after edits so deleted
// entries do not survive there. Pins are user preferences and stay separate.
func (s *shell) syncPaletteHistoryRecents() {
	h := &s.palette.history
	var commands, shellLines []string
	uses := make(map[commandID]int)
	for _, entry := range h.Entries {
		if entry.Kind == paletteHistoryCommand && entry.Command != "" {
			uses[entry.Command]++
			commands = append(commands, string(entry.Command))
		} else if entry.Kind == paletteHistoryShell {
			shellLines = append(shellLines, entry.Text)
		}
	}
	s.palette.recent = s.palette.recent[:0]
	// Entries are appended chronologically. The imported legacy entries are
	// reversed once by the loader.
	slices.Reverse(commands)
	slices.Reverse(shellLines)
	for _, id := range normalizeCommandList(commands, 20) {
		s.palette.recent = append(s.palette.recent, commandID(id))
	}
	s.palette.shellHistory = normalizeCommandList(shellLines, 50)
	s.palette.uses = uses
	s.trimCommandUses()
}

func (s *shell) deletePaletteHistory(clearAll bool) {
	h := &s.palette.history
	if h.loadFailed {
		return
	}
	before := h.Entries
	if clearAll {
		h.Entries = nil
	} else {
		h.Entries = slices.DeleteFunc(slices.Clone(before), func(entry paletteHistoryEntry) bool { return h.selected[entry.ID] })
	}
	if err := h.save(); err != nil {
		h.Entries = before
		h.err = safeError(err)
		return
	}
	h.err, h.clear, h.multi, h.selected = "", false, false, nil
	s.syncPaletteHistoryRecents()
	s.savePaletteState()
}

func (s *shell) togglePaletteHistory(open bool) {
	h := &s.palette.history
	h.open, h.clear, h.multi, h.selected = open, false, false, nil
	if open {
		h.filter, h.cursor, h.focusList = "", 0, true
		h.list.ScrollTo(0, ui.Start)
		s.rememberPaletteQuery(s.activeApp(), s.palette.query)
	} else {
		s.palette.revealSelection = true
	}
}

func (s *shell) paletteHistoryView(c *ui.Context) {
	h := &s.palette.history
	theme := c.Theme()
	ui.Row(c).Gap(8).AlignItems(ui.Center).Wrap().Children(func() {
		ui.TextInput(c, &h.filter).Grow(1).MinWidth(100).Placeholder(tr("Find in history")).Label(tr("Find in history")).
			OnShortcut(0, ui.KeyDown, func() {
				h.focusList = true
				h.cursor = 0
			})
		toggle := ui.Switch(c, &h.Paused).Label(tr("Pause recording")).Disabled(h.loadFailed)
		toggle.OnChange(func() { h.err = safeError(h.save()) })
		ui.Text(c, tr("Pause recording")).SingleLine()
	})
	ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
		ui.Text(c, tr("Newest first")).TextColor(theme.TextMuted).Grow(1)
		if h.multi {
			ui.Text(c, trf("%d selected", len(h.selected))).TextColor(theme.TextMuted)
		}
		selectButton := compactIconButton(ui.ButtonBase(c)).Label(tr("Select history entries")).Tooltip(tr("Select history entries")).
			Checked(h.multi).Disabled(len(h.Entries) == 0).Radius(theme.Radius)
		if h.multi {
			selectButton.Background(theme.Accent.Alpha(0.2))
		} else {
			selectButton.Background(theme.Surface)
		}
		selectButton.OnClick(func() {
			h.multi = !h.multi
			h.selected = nil
			h.focusList = true
		}).Children(func() { ui.Icon(c, selectCommitsIcon).FontSize(14) })
		if h.multi {
			iconButton(c, "Delete selected", "Delete selected", trashIcon, len(h.selected) == 0 || h.loadFailed, func() {
				s.deletePaletteHistory(false)
			})
		}
		iconButton(c, "Clear history", "Clear history", clearHistoryIcon, len(h.Entries) == 0 || h.loadFailed || h.clear, func() {
			h.clear = true
		})
	})
	if h.err != "" {
		ui.Text(c, h.err).TextColor(theme.Danger)
	}
	needle := strings.ToLower(strings.TrimSpace(h.filter))
	entries := h.Entries
	count := len(entries)
	entryAt := func(row int) paletteHistoryEntry { return entries[count-1-row] }
	if needle != "" {
		var indices []int
		for i := len(entries) - 1; i >= 0; i-- {
			entry := entries[i]
			if strings.Contains(strings.ToLower(paletteHistoryText(entry)+" "+entry.Text+" "+entry.Workspace), needle) {
				indices = append(indices, i)
			}
		}
		count = len(indices)
		entryAt = func(row int) paletteHistoryEntry { return entries[indices[row]] }
	}
	ui.Divider(c)
	if count == 0 {
		ui.Column(c).Grow(1).Center().Children(func() {
			ui.Text(c, tr("No history entries")).TextColor(theme.TextMuted)
		})
	} else {
		h.list.Selected = &h.cursor
		h.list.RowHighlighted = func(int) bool { return false }
		h.cursor = min(max(h.cursor, 0), count-1)
		list := ui.List(c, &h.list, count).Grow(1).FillWidth().Label(tr("History"))
		list.ItemKey(func(index int) any { return entryAt(index).ID })
		list.OnSubmit(func() {
			if !h.clear && h.cursor >= 0 && h.cursor < count {
				s.activatePaletteHistoryRow(entryAt(h.cursor))
			}
		})
		list.Rows(func(row ui.ListRow) { s.paletteHistoryRow(row, entryAt(row.Index)) })
		if h.focusList {
			h.list.Focus(c)
			h.focusList = false
		}
	}
	if h.clear {
		ui.Divider(c)
		ui.Row(c).Gap(8).AlignItems(ui.Center).Wrap().Children(func() {
			ui.Text(c, tr("Clear all command palette history?")).Grow(1)
			ui.Button(c, tr("Cancel")).OnClick(func() { h.clear = false })
			iconTextButton(c, "Confirm clear history", "Confirm clear history", trashIcon, false, func() { s.deletePaletteHistory(true) })
		})
	}
}

func paletteHistoryText(entry paletteHistoryEntry) string {
	if path, ok := strings.CutPrefix(string(entry.Command), "files/open/"); ok {
		return trf("Open %s", filepath.Base(path))
	}
	if entry.Text == "history/search-typed-code-changes" {
		return tr(historyChangesLabel)
	}
	return entry.Text
}

func (s *shell) activatePaletteHistoryRow(entry paletteHistoryEntry) {
	h := &s.palette.history
	if h.multi {
		if h.selected == nil {
			h.selected = make(map[uint64]bool)
		}
		if h.selected[entry.ID] {
			delete(h.selected, entry.ID)
		} else {
			h.selected[entry.ID] = true
		}
		return
	}
	s.recallPaletteHistory(entry)
}

func (s *shell) paletteHistoryRow(row ui.ListRow, entry paletteHistoryEntry) {
	h, c := &s.palette.history, row.Context
	theme := c.Theme()
	text := paletteHistoryText(entry)
	button := ui.ButtonBase(c).FillWidth().AlignItems(ui.Center).Gap(10).Padding(7, 9).
		Radius(theme.Radius).Label(text).Disabled(h.clear)
	if row.Index == h.cursor && row.ListFocused() {
		button.Background(theme.Accent.Alpha(0.16)).Border(1, theme.Accent.Alpha(0.45))
	} else if button.Hovered() || h.selected[entry.ID] {
		button.Background(theme.SurfaceHover)
	}
	button.OnClick(func() {
		h.list.Choose(row.Index)
		s.activatePaletteHistoryRow(entry)
	}).Children(func() {
		if h.multi {
			checked := h.selected[entry.ID]
			ui.Checkbox(c, &checked, "").Label(tr("Select history entries") + " " + text).OnClick(func() {
				h.list.Choose(row.Index)
				s.activatePaletteHistoryRow(entry)
			})
		} else {
			glyph := searchIcon
			switch entry.Kind {
			case paletteHistoryShell:
				glyph = terminalIcon
			case paletteHistoryCommand:
				glyph = commandIcon
			}
			ui.Icon(c, glyph).FontSize(15).TextColor(theme.TextMuted)
		}
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, text).SingleLine().Tooltip(text)
			detail := entry.Workspace
			if path, ok := strings.CutPrefix(string(entry.Command), "files/open/"); ok {
				detail = path
			}
			if detail != "" {
				ui.Text(c, detail).TextColor(theme.TextMuted).SingleLine().Tooltip(detail)
			}
		})
		if !entry.At.IsZero() {
			ui.Text(c, relTime(entry.At.Unix())).TextColor(theme.TextMuted).SingleLine()
		}
		if !h.multi {
			ui.Icon(c, chevronRightIcon).FontSize(13).TextColor(theme.TextMuted)
		}
	})
}

func paletteHistoryWorkspace(a *app) string {
	if a.location.IsRemote() {
		location := a.location
		if a.snapshot.Root != "" {
			location.Root = a.snapshot.Root
		}
		return location.String()
	}
	return a.snapshot.Root
}

func (s *shell) recallPaletteHistory(entry paletteHistoryEntry) {
	h := &s.palette.history
	a := s.activeApp()
	if a == nil {
		return
	}
	if entry.Workspace != "" && entry.Workspace != paletteHistoryWorkspace(a) {
		for index, tab := range s.tabs {
			if tab != nil && tab.app != nil && paletteHistoryWorkspace(tab.app) == entry.Workspace {
				s.activate(index)
				a = tab.app
				break
			}
		}
		if entry.Workspace != paletteHistoryWorkspace(a) {
			h.err = tr("Open the original workspace to run this again")
			return
		}
	}
	h.err = ""
	switch entry.Kind {
	case paletteHistoryCode:
		if entry.CodeQuery != nil && a.historyChangesAvailable() {
			s.closeCommandPalette(true)
			a.openHistoryChanges(entry.CodeQuery)
			a.startHistoryChanges()
		} else {
			h.err = tr("Code change search requires the History view")
		}
	case paletteHistorySearch:
		s.palette.query, s.palette.selected, s.palette.target = entry.Text, 0, nil
		s.togglePaletteHistory(false)
	case paletteHistoryShell, paletteHistoryCommand:
		s.runPaletteHistoryCommand(a, entry)
	}
}

func (s *shell) runPaletteHistoryCommand(a *app, entry paletteHistoryEntry) {
	id, query := entry.Command, entry.Query
	if entry.Kind == paletteHistoryShell {
		query = "! " + entry.Text
		if id == "" {
			id = "commands/run-line"
		}
	}
	if id == "history/search-typed-code-changes" {
		id = "history/search-code-changes"
	}
	if path, ok := strings.CutPrefix(string(id), "files/open/"); ok {
		query = "/" + path
		s.palette.filesChangedOnly = false
	} else if strings.HasPrefix(string(id), "branches/checkout/") || strings.HasPrefix(string(id), "branches/remote/") {
		query = "@"
	}
	if hash, ok := strings.CutPrefix(string(id), "history/commit/"); ok {
		switch {
		case entry.Action == 2:
			a.notifyCopied(a.services, "commit hash", hash)
		case entry.Action == 0 && entry.Commit != nil:
			s.openCommitActions(a, *entry.Commit)
			s.togglePaletteHistory(false)
		default:
			s.closeCommandPalette(true)
			a.showCommitInHistory(hash)
		}
		return
	}
	oldQuery, oldTarget := s.palette.query, s.palette.target
	s.palette.query, s.palette.target = query, nil
	if entry.Commit != nil {
		s.palette.target = &paletteCommitTarget{app: a, commit: *entry.Commit}
	}
	s.palette.history.lastQuery = strings.TrimSpace(query)
	result := s.invokeCommandAction(id, entry.Action)
	if !result.Accepted() {
		s.palette.query, s.palette.target = oldQuery, oldTarget
		s.palette.history.err = tr(result.Reason)
		return
	}
	s.togglePaletteHistory(false)
}

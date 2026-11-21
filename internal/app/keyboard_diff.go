package app

import (
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

type diffKeyboardCommand struct {
	keys, label string
	mods        ui.Modifiers
	key         ui.Key
	selection   bool
	changesHunk bool
	run         func(*app, *editor.DiffState)
}

func diffKeyboardCommands() []diffKeyboardCommand {
	return []diffKeyboardCommand{
		{"j", "Move down in the diff", 0, ui.KeyJ, false, false, func(a *app, s *editor.DiffState) {
			if a.lineSelectMode {
				a.selectKeyboardDiffLines(s, 1, true)
			} else {
				s.Run(editor.MoveDown)
			}
		}},
		{"k", "Move up in the diff", 0, ui.KeyK, false, false, func(a *app, s *editor.DiffState) {
			if a.lineSelectMode {
				a.selectKeyboardDiffLines(s, -1, true)
			} else {
				s.Run(editor.MoveUp)
			}
		}},
		{"m", "Turn selecting changes on or off: hunk actions and line picking", 0, ui.KeyM, false, true, func(a *app, s *editor.DiffState) {
			if s == a.diffState {
				a.toggleSelectionMode()
			}
		}},
		{"v", "Start a diff-line selection", 0, ui.KeyV, true, false, func(a *app, s *editor.DiffState) { a.selectKeyboardDiffLines(s, 0, false) }},
		{"Shift+Down", "Extend the diff-line selection down", ui.Shift, ui.KeyDown, true, false, func(a *app, s *editor.DiffState) { a.selectKeyboardDiffLines(s, 1, true) }},
		{"Shift+Up", "Extend the diff-line selection up", ui.Shift, ui.KeyUp, true, false, func(a *app, s *editor.DiffState) { a.selectKeyboardDiffLines(s, -1, true) }},
		{"Space", "Stage or unstage selected diff lines", 0, ui.KeySpace, true, false, func(a *app, s *editor.DiffState) {
			if _, ok := s.SelectedLines(); !ok {
				a.selectKeyboardDiffLines(s, 0, false)
			}
			if s == a.commitDiffState {
				a.addCustomPatchSelectedLines()
			} else {
				a.beginLineAction()
			}
		}},
		{"s", "Stage or unstage the current diff hunk", 0, ui.KeyS, false, true, func(a *app, s *editor.DiffState) {
			if s == a.diffState {
				a.beginCurrentHunkAction()
			}
		}},
		{"d", "Discard the current working-tree diff hunk after confirmation", 0, ui.KeyD, false, true, func(a *app, s *editor.DiffState) {
			if s == a.diffState {
				a.confirmDiscardCurrentHunk()
			}
		}},
		{"]", "Go to the next diff hunk", 0, ui.KeyBracketRight, false, false, func(a *app, s *editor.DiffState) { navigateDiffHunk(s, 1) }},
		{"[", "Go to the previous diff hunk", 0, ui.KeyBracketLeft, false, false, func(a *app, s *editor.DiffState) { navigateDiffHunk(s, -1) }},
	}
}

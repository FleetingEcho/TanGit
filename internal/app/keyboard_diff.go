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
	run         func(*app, *editor.DiffState)
}

func diffKeyboardCommands() []diffKeyboardCommand {
	return []diffKeyboardCommand{
		{"j", "Move down in the diff", 0, ui.KeyJ, false, func(a *app, s *editor.DiffState) {
			if a.lineSelectMode {
				a.selectKeyboardDiffLines(s, 1, true)
			} else {
				s.Run(editor.MoveDown)
			}
		}},
		{"k", "Move up in the diff", 0, ui.KeyK, false, func(a *app, s *editor.DiffState) {
			if a.lineSelectMode {
				a.selectKeyboardDiffLines(s, -1, true)
			} else {
				s.Run(editor.MoveUp)
			}
		}},
		{"v", "Start a diff-line selection", 0, ui.KeyV, true, func(a *app, s *editor.DiffState) { a.selectKeyboardDiffLines(s, 0, false) }},
		{"Shift+Down", "Extend the diff-line selection down", ui.Shift, ui.KeyDown, true, func(a *app, s *editor.DiffState) { a.selectKeyboardDiffLines(s, 1, true) }},
		{"Shift+Up", "Extend the diff-line selection up", ui.Shift, ui.KeyUp, true, func(a *app, s *editor.DiffState) { a.selectKeyboardDiffLines(s, -1, true) }},
		{"Space", "Stage or unstage selected diff lines", 0, ui.KeySpace, true, func(a *app, s *editor.DiffState) {
			if _, ok := s.SelectedLines(); !ok {
				a.selectKeyboardDiffLines(s, 0, false)
			}
			if s == a.commitDiffState {
				a.addCustomPatchSelectedLines()
			} else {
				a.beginLineAction()
			}
		}},
		{"]", "Go to the next diff hunk", 0, ui.KeyBracketRight, false, func(a *app, s *editor.DiffState) { h, _ := s.CurrentHunk(); s.GoToHunk(min(h+1, len(s.Hunks())-1)) }},
		{"[", "Go to the previous diff hunk", 0, ui.KeyBracketLeft, false, func(a *app, s *editor.DiffState) { h, _ := s.CurrentHunk(); s.GoToHunk(max(0, h-1)) }},
	}
}

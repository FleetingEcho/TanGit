package app

import "testing"

func TestCommandPaletteModesAndShellEntry(t *testing.T) {
	h, _ := menuTestAppWithCalls(t)
	s := newShell(nil)
	ctx := commandContext{Shell: s, App: h.app}

	for _, id := range []commandID{"commands/run", "commands/customize"} {
		if _, ok := catalogCandidate(s.catalogFor(ctx).Search(ctx, string(id), nil), id); !ok {
			t.Errorf("%s is missing from the palette", id)
		}
	}
	if mode, rest := commandQueryMode("  !git log"); mode != '!' || rest != "git log" {
		t.Fatalf("shell mode = %q %q", mode, rest)
	}
	if mode, rest := commandQueryMode("@ main"); mode != '@' || rest != "main" {
		t.Fatalf("branch mode = %q %q", mode, rest)
	}

	s.palette.open = true
	s.palette.query = "!echo hi"
	catalog := s.catalogFor(ctx)
	got := catalog.Search(ctx, s.palette.query, nil)
	if len(got) != 1 || got[0].ID != "commands/run-line" || got[0].Label != "echo hi" {
		t.Fatalf("shell mode candidates = %+v", got)
	}
	// Prefixed commands never leak into the default list.
	for _, c := range catalog.Search(ctx, "", nil) {
		if c.ID == "commands/run-line" {
			t.Fatal("shell line appeared in the default list")
		}
	}
}

func TestShiftedKeyboardChordsSpellOutShift(t *testing.T) {
	h, _ := menuTestAppWithCalls(t)
	s := newShell(nil)
	ctx := commandContext{Shell: s, App: h.app}
	redo, ok := catalogCandidate(s.catalogFor(ctx).Search(ctx, "Redo the latest undone", nil), "git/Everywhere/Redo the latest undone Git operation")
	if !ok || redo.Chord != "Shift+Z" {
		t.Fatalf("redo chord = %+v", redo)
	}
}

func TestPinnedCommandsLeadTheEmptyList(t *testing.T) {
	h, _ := menuTestAppWithCalls(t)
	s := newShell(nil)
	ctx := commandContext{Shell: s, App: h.app}
	s.togglePinnedCommand("theme/Tokyo Night")
	got := s.catalogFor(ctx).SearchPinned(ctx, "", nil, s.palette.pinned)
	if len(got) == 0 || got[0].ID != "theme/Tokyo Night" || !got[0].Pinned {
		t.Fatalf("pinned command is not first: %+v", got[:min(len(got), 2)])
	}
}

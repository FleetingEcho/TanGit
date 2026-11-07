package app

import "testing"

// Linux gets the physical ? key through GTK's accelerator mapping, rather than
// MyGo's key-event mapping, which drops this shifted punctuation in v0.7.4.
func TestNativeHelpMenuUsesQuestionMarkAccelerator(t *testing.T) {
	s := newShell(nil)
	s.tabs = []*tab{{app: newApp(nil)}}
	menus := nativeHelpMenu(s).Items()
	if len(menus) != 1 || len(menus[0].Submenu) != 1 {
		t.Fatal("the native Help menu does not contain exactly one shortcut action")
	}
	item := menus[0].Submenu[0]
	if item.Accelerator != "Shift+/" {
		t.Fatalf("the native help accelerator is %q, want Shift+/", item.Accelerator)
	}
	if item.Click == nil {
		t.Fatal("the native help accelerator has no action")
	}

	item.Click(item, nil)
	if s.activeApp().keybindingsOpen {
		t.Fatal("the native action changed the page while a frame could be reading it")
	}
	s.applyPending()
	if !s.activeApp().keybindingsOpen || !s.activeApp().keybindingsCurrentOnly {
		t.Fatal("the native ? action did not open current-page shortcuts")
	}
}

// Native menu accelerators do not pass through registerShortcuts. Enforce its
// disabled state and modal ownership here so the platform workaround cannot
// open help over a prompt or a form that owns the keyboard.
func TestNativeHelpShortcutRespectsShortcutScope(t *testing.T) {
	tests := []struct {
		name          string
		prepare       func(*app)
		wantSettings  bool
		wantHelp      bool
		wantCurrent   bool
		wantFormStill bool
	}{
		{
			name: "shortcuts disabled",
			prepare: func(a *app) {
				a.settings.DisableShortcuts = true
			},
		},
		{
			name: "Settings hands over to page help",
			prepare: func(a *app) {
				a.openSettings()
			},
			wantHelp:    true,
			wantCurrent: true,
		},
		{
			name: "reference switches to page help",
			prepare: func(a *app) {
				a.openKeybindings()
			},
			wantHelp:    true,
			wantCurrent: true,
		},
		{
			name: "confirmation keeps the keyboard",
			prepare: func(a *app) {
				a.confirm = &confirmRequest{}
			},
		},
		{
			name: "form keeps the keyboard",
			prepare: func(a *app) {
				a.showCommitForm = true
			},
			wantFormStill: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := newShell(nil)
			a := newApp(nil)
			s.tabs = []*tab{{app: a}}
			test.prepare(a)
			s.openNativeCurrentKeybindings()
			if a.settingsOpen != test.wantSettings {
				t.Errorf("Settings open = %v, want %v", a.settingsOpen, test.wantSettings)
			}
			if a.keybindingsOpen != test.wantHelp || a.keybindingsCurrentOnly != test.wantCurrent {
				t.Errorf("help open/current = %v/%v, want %v/%v", a.keybindingsOpen, a.keybindingsCurrentOnly, test.wantHelp, test.wantCurrent)
			}
			if a.showCommitForm != test.wantFormStill {
				t.Errorf("commit form open = %v, want %v", a.showCommitForm, test.wantFormStill)
			}
		})
	}
}

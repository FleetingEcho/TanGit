package app

import (
	"fmt"

	"github.com/FleetingEcho/lazygitGo/internal/app/i18n"
)

// languageChoice is one entry of the language menu: the code the settings
// store, and the name the menu shows in that language.
type languageChoice = i18n.Choice

// defaultLanguage is the language the app starts in. The settings modal
// changes it, and the choice is stored with the other preferences.
const defaultLanguage = i18n.DefaultLanguage

// languageChoices are the languages the app supports, in menu order.
var languageChoices = i18n.Choices

// currentLanguage is the language the frame being built is drawn in. MyGo
// builds every window on the main thread, so one value serves them all; each
// frame settles it from the window's settings before anything else builds.
var currentLanguage = defaultLanguage

// normalizeLanguage maps a stored code to a supported language, falling back
// to English so a damaged or older settings file still starts.
func normalizeLanguage(code string) string { return i18n.Normalize(code) }

// languageName is the menu label of a stored language code.
func languageName(code string) string { return i18n.Name(code) }

// tr translates an English UI string into the current language. English is
// the identity, and a string with no translation stays as written, so a
// missing entry never blanks a label. Add each new literal to every catalog
// in the same change; the source-coverage test rejects omissions. Use trf
// for dynamic values instead of constructing translation keys at runtime.
func tr(text string) string { return i18n.Tr(text, currentLanguage) }

// trf translates a format string before filling it, so the translated text
// can order the values its own way.
func trf(format string, args ...any) string {
	return fmt.Sprintf(tr(format), args...)
}

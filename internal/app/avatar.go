package app

import (
	"hash/fnv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

// avatarPalette is the set of colors a generated author avatar picks from.
// They do not follow the theme, as GitHub's identicons do not, so a person
// keeps the same color whichever palette the window shows, and the white
// initial stays readable on every one of them.
var avatarPalette = []ui.Color{
	ui.Hex("#3b63d9"), // blue
	ui.Hex("#6e4fe0"), // violet
	ui.Hex("#b21fc2"), // magenta
	ui.Hex("#cf3439"), // red
	ui.Hex("#bd4f26"), // orange
	ui.Hex("#9a5f17"), // amber
	ui.Hex("#2c7c44"), // green
	ui.Hex("#0e7a70"), // teal
	ui.Hex("#2470ab"), // steel blue
	ui.Hex("#bf3368"), // pink
	ui.Hex("#5b6b84"), // slate
	ui.Hex("#855e53"), // brown
}

// avatarTextColor is the initial's color; every palette color is dark enough
// for white, in either theme.
var avatarTextColor = ui.RGB(255, 255, 255)

// authorAvatarBitmap returns the cached hosting photo for a commit. The
// caller owns the non-blocking cache lookup and supplies only its redraw hook,
// never a Context or Element, so painter and widget callers share one store.
func (a *app) authorAvatarBitmap(commit repository.Commit, invalidate func()) *ui.Bitmap {
	if a.authorAvatars == nil {
		return nil
	}
	remotes := a.snapshot.Remotes
	// Prefer origin; a fork's upstream must not override its configured host.
	for pass := 0; pass < 2; pass++ {
		found := false
		for _, remote := range remotes {
			if (remote.Name == "origin") != (pass == 0) {
				continue
			}
			if source, ok := avatarSourceFor(a.remoteWebAddress(remote), commit); ok {
				bitmap := a.authorAvatars.lookup(source, invalidate)
				found = true
				if bitmap != nil {
					return bitmap
				}
				break
			}
		}
		if found {
			break
		}
	}
	return nil
}

// authorAvatar shows a cached hosting photo when available, and keeps the
// generated initial while a lookup is pending or unavailable.
func (a *app) authorAvatar(c *ui.Context, commit repository.Commit, size float32) {
	bitmap := a.authorAvatarBitmap(commit, c.Services().Invalidate)
	drawAuthorAvatar(c, commit.AuthorName, commit.AuthorEmail, size, bitmap)
}

func drawAuthorAvatar(c *ui.Context, name, email string, size float32, bitmap *ui.Bitmap) {
	initial := authorInitial(name, email)
	if bitmap == nil && initial == "?" {
		// The native initials intentionally produce no glyph for punctuation.
		// Preserve our visible unknown-author fallback for that one case.
		avatar := ui.Row(c).Width(size).Height(size).Radius(size / 2).
			Background(avatarColor(name, email)).Center().
			Label(authorLabel(name, email)).Shrink(0).Clip()
		avatar.Children(func() {
			ui.Text(c, "?").Bold().FontSize(size * 0.5).TextColor(avatarTextColor)
		})
		return
	}
	// Native Avatar owns the circular crop and photo path. Passing the
	// established initial/palette preserves lazygitGo's identity across themes
	// and its readable, single-letter presentation for ordinary authors.
	if bitmap != nil {
		// The native Avatar keeps its own radius and photo size, so any other
		// size turns the circle into a rounded square. A photo covers the whole
		// circle, so it also gets no palette fill, which would only show as a
		// colored fringe on the anti-aliased edge.
		photo := ui.Row(c).Width(size).Height(size).Radius(size / 2).Center().
			Label(authorLabel(name, email)).Shrink(0).Clip()
		photo.Children(func() {
			ui.Image(c, bitmap).Fit(ui.Cover).Size(size, size)
		})
		return
	}
	ui.Avatar(c, initial, nil).Size(size, size).Radius(size / 2).
		Background(avatarColor(name, email)).Label(authorLabel(name, email))
}

// authorInitial is the letter the avatar shows: the first letter of the
// author's first name, of the email's local part when the name says nothing,
// and "?" when neither does.
func authorInitial(name, email string) string {
	for _, field := range []string{name, email} {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		r, _ := utf8.DecodeRuneInString(field)
		if r == utf8.RuneError || (!unicode.IsLetter(r) && !unicode.IsDigit(r)) {
			continue
		}
		return string(unicode.ToUpper(r))
	}
	return "?"
}

// avatarColor is the avatar's color, chosen from the palette by a hash of the
// author's email, so the same person keeps it from one commit to the next and
// from one run to the next.
func avatarColor(name, email string) ui.Color {
	seed := strings.ToLower(strings.TrimSpace(email))
	if seed == "" {
		seed = strings.ToLower(strings.TrimSpace(name))
	}
	if seed == "" {
		return avatarPalette[0]
	}
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(seed))
	// FNV leaves strings that differ in one byte close together, so equal
	// authors would cluster on one color; the finalizer spreads them.
	h := hash.Sum32()
	h ^= h >> 16
	h *= 0x7feb352d
	h ^= h >> 15
	return avatarPalette[h%uint32(len(avatarPalette))]
}

// authorLabel is what assistive technology calls the avatar: the author's
// name, or the email when the name is empty.
func authorLabel(name, email string) string {
	if name = strings.TrimSpace(name); name != "" {
		return name
	}
	return strings.TrimSpace(email)
}

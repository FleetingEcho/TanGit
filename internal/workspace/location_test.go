package workspace

import (
	"path/filepath"
	"testing"
)

func TestParseLocalLocations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
		root  string
	}{
		{name: "absolute path", value: "/work/project", root: "/work/project"},
		{name: "relative path", value: "work/project", root: "work/project"},
		{name: "surrounding whitespace", value: "  /work/my project\t\n", root: "/work/my project"},
		{name: "interior whitespace", value: "my project", root: "my project"},
		{name: "HTTPS URI", value: "https://example.com/work", root: "https://example.com/work"},
		{name: "file URI", value: "file:///work/project", root: "file:///work/project"},
		{name: "SSH without URI slashes", value: "ssh:work/project", root: "ssh:work/project"},
		{name: "colon in local value", value: `C:\work\project`, root: `C:\work\project`},
		{name: "dot", value: ".", root: "."},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			location, err := Parse(test.value)
			if err != nil {
				t.Fatalf("Parse(%q) returned error: %v", test.value, err)
			}
			want := Location{Kind: Local, Root: test.root}
			if location != want {
				t.Fatalf("Parse(%q) = %#v, want %#v", test.value, location, want)
			}
			if location.String() != test.root {
				t.Fatalf("String() = %q, want %q", location.String(), test.root)
			}
			if location.IsRemote() {
				t.Fatal("local location reports remote")
			}
			if !location.Valid() {
				t.Fatal("local location is invalid")
			}
		})
	}
}

func TestParseRejectsEmptyLocalLocations(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"", " ", "\t\r\n"} {
		if location, err := Parse(value); err == nil {
			t.Errorf("Parse(%q) = %#v, want an error", value, location)
		}
	}
}

func TestSSHLocationRoundTripsSpacesAndUnicode(t *testing.T) {
	t.Parallel()

	location, err := Parse("  ssh://Profile_1/work/my%20project/%E6%9D%B1%E4%BA%AC%20repo  ")
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	want := Location{Kind: SSH, ProfileID: "Profile_1", Root: "/work/my project/東京 repo"}
	if location != want {
		t.Fatalf("Parse = %#v, want %#v", location, want)
	}
	const canonical = "ssh://Profile_1/work/my%20project/%E6%9D%B1%E4%BA%AC%20repo"
	if got := location.String(); got != canonical {
		t.Fatalf("String() = %q, want %q", got, canonical)
	}
	roundTripped, err := Parse(location.String())
	if err != nil {
		t.Fatalf("Parse(String()) returned error: %v", err)
	}
	if roundTripped != location {
		t.Fatalf("Parse(String()) = %#v, want %#v", roundTripped, location)
	}
	if !location.Valid() || !location.IsRemote() {
		t.Fatalf("SSH location Valid() = %v, IsRemote() = %v", location.Valid(), location.IsRemote())
	}
}

func TestSSHLocationAcceptsLiteralUnicodeAndEscapesReservedPathCharacters(t *testing.T) {
	t.Parallel()

	location, err := Parse("ssh://profile/work/café/a%23b%3Fc%25d")
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if want := "/work/café/a#b?c%d"; location.Root != want {
		t.Fatalf("Root = %q, want %q", location.Root, want)
	}
	roundTripped, err := Parse(location.String())
	if err != nil {
		t.Fatalf("Parse(String()) returned error for %q: %v", location.String(), err)
	}
	if roundTripped != location {
		t.Fatalf("Parse(String()) = %#v, want %#v", roundTripped, location)
	}
}

func TestSSHLocationCanonicalizesRoot(t *testing.T) {
	t.Parallel()

	location, err := Parse("ssh://profile/work/./project/../repo")
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if location.Root != "/work/repo" {
		t.Fatalf("Root = %q, want /work/repo", location.Root)
	}
	if got := location.String(); got != "ssh://profile/work/repo" {
		t.Fatalf("String() = %q, want canonical URI", got)
	}
}

func TestSSHLocationsWithDifferentProfilesRemainDistinct(t *testing.T) {
	t.Parallel()

	first, err := Parse("ssh://work/work/repo")
	if err != nil {
		t.Fatal(err)
	}
	second, err := Parse("ssh://Work/work/repo")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("locations with different profile IDs are equal: %#v", first)
	}
	if first.ProfileID != "work" || second.ProfileID != "Work" {
		t.Fatalf("profile case was not preserved: %q and %q", first.ProfileID, second.ProfileID)
	}
	if first.Root != second.Root {
		t.Fatalf("roots differ: %q and %q", first.Root, second.Root)
	}
}

func TestParseRejectsMalformedSSHLocations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
	}{
		{name: "missing profile", value: "ssh:///work/repo"},
		{name: "empty URI", value: "ssh://"},
		{name: "missing root", value: "ssh://profile"},
		{name: "username", value: "ssh://user@profile/work/repo"},
		{name: "credentials", value: "ssh://user:secret@profile/work/repo"},
		{name: "empty user info", value: "ssh://@profile/work/repo"},
		{name: "query", value: "ssh://profile/work/repo?token=secret"},
		{name: "empty query", value: "ssh://profile/work/repo?"},
		{name: "fragment", value: "ssh://profile/work/repo#fragment"},
		{name: "empty fragment", value: "ssh://profile/work/repo#"},
		{name: "profile starts with dash", value: "ssh://-profile/work"},
		{name: "profile starts with dot", value: "ssh://.profile/work"},
		{name: "profile starts with underscore", value: "ssh://_profile/work"},
		{name: "port", value: "ssh://profile:22/work"},
		{name: "space in profile", value: "ssh://bad profile/work"},
		{name: "percent in profile", value: "ssh://bad%profile/work"},
		{name: "NUL in root", value: "ssh://profile/work%00repo"},
		{name: "newline in root", value: "ssh://profile/work%0Arepo"},
		{name: "Unicode control in root", value: "ssh://profile/work\u0085repo"},
		{name: "malformed escape", value: "ssh://profile/work/%zz"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if location, err := Parse(test.value); err == nil {
				t.Fatalf("Parse(%q) = %#v, want an error", test.value, location)
			}
		})
	}
}

func TestManuallyConstructedLocationValidityAndString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		location Location
		valid    bool
		str      string
	}{
		{name: "zero value", location: Location{}, str: ""},
		{name: "unknown kind", location: Location{Kind: "other", Root: "/work"}, str: ""},
		{name: "empty local root", location: Location{Kind: Local}, str: ""},
		{name: "valid local root", location: Location{Kind: Local, Root: " relative path "}, valid: true, str: " relative path "},
		{name: "missing SSH profile", location: Location{Kind: SSH, Root: "/work"}, str: ""},
		{name: "empty SSH root", location: Location{Kind: SSH, ProfileID: "profile"}, str: ""},
		{name: "relative SSH root", location: Location{Kind: SSH, ProfileID: "profile", Root: "work/repo"}, str: ""},
		{name: "dot-relative SSH root", location: Location{Kind: SSH, ProfileID: "profile", Root: "../work"}, str: ""},
		{name: "profile with slash", location: Location{Kind: SSH, ProfileID: "bad/profile", Root: "/work"}, str: ""},
		{name: "profile with colon", location: Location{Kind: SSH, ProfileID: "bad:profile", Root: "/work"}, str: ""},
		{name: "profile with percent", location: Location{Kind: SSH, ProfileID: "bad%profile", Root: "/work"}, str: ""},
		{name: "profile with whitespace", location: Location{Kind: SSH, ProfileID: "bad profile", Root: "/work"}, str: ""},
		{name: "profile with Unicode", location: Location{Kind: SSH, ProfileID: "profilé", Root: "/work"}, str: ""},
		{name: "NUL in SSH root", location: Location{Kind: SSH, ProfileID: "profile", Root: "/work\x00repo"}, str: ""},
		{name: "control in SSH root", location: Location{Kind: SSH, ProfileID: "profile", Root: "/work\u007frepo"}, str: ""},
		{name: "noncanonical SSH root", location: Location{Kind: SSH, ProfileID: "Profile-2", Root: "/work/./project/../repo"}, valid: true, str: "ssh://Profile-2/work/repo"},
		{name: "filesystem root", location: Location{Kind: SSH, ProfileID: "profile", Root: "/"}, valid: true, str: "ssh://profile/"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.location.Valid(); got != test.valid {
				t.Errorf("Valid() = %v, want %v", got, test.valid)
			}
			if got := test.location.String(); got != test.str {
				t.Errorf("String() = %q, want %q", got, test.str)
			}
		})
	}
}

func TestLocationBase(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		location Location
		want     string
	}{
		{name: "local absolute path", location: Location{Kind: Local, Root: filepath.Join(string(filepath.Separator), "work", "project") + string(filepath.Separator)}, want: "project"},
		{name: "local filesystem root", location: Location{Kind: Local, Root: string(filepath.Separator)}, want: string(filepath.Separator)},
		{name: "local dot", location: Location{Kind: Local, Root: "."}, want: "."},
		{name: "remote path", location: Location{Kind: SSH, ProfileID: "profile", Root: "/work/my project"}, want: "my project"},
		{name: "remote trailing slash", location: Location{Kind: SSH, ProfileID: "profile", Root: "/work/東京/"}, want: "東京"},
		{name: "remote noncanonical path", location: Location{Kind: SSH, ProfileID: "profile", Root: "/work/project/../repo"}, want: "repo"},
		{name: "remote filesystem root", location: Location{Kind: SSH, ProfileID: "profile", Root: "/"}, want: "/"},
		{name: "invalid location", location: Location{}, want: ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.location.Base(); got != test.want {
				t.Errorf("Base() = %q, want %q", got, test.want)
			}
		})
	}
}

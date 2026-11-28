package app

import "testing"

func TestDefaultHostingIssueLink(t *testing.T) {
	tests := []struct {
		name, provider, projectURL, want string
	}{
		{
			name:       "GitLab",
			provider:   hostingProviderGitLab,
			projectURL: "https://gitlab.example.com/group/project.git",
			want:       "https://gitlab.example.com/group/project/-/issues/{id}",
		},
		{
			name:       "GitHub enterprise",
			provider:   hostingProviderGitHub,
			projectURL: "https://github.example.com/team/project?tab=readme#about",
			want:       "https://github.example.com/team/project/issues/{id}",
		},
		{name: "server without project", provider: hostingProviderGitLab, projectURL: "https://gitlab.example.com", want: ""},
		{name: "unsafe scheme", provider: hostingProviderGitHub, projectURL: "file:///team/project", want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := defaultHostingIssueLink(test.provider, test.projectURL); got != test.want {
				t.Fatalf("defaultHostingIssueLink() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestDefaultHostingIssuesURL(t *testing.T) {
	tests := []struct {
		provider, projectURL, want string
	}{
		{hostingProviderGitLab, "https://gitlab.example.com/group/project.git", "https://gitlab.example.com/group/project/-/issues"},
		{hostingProviderGitHub, "https://github.example.com/team/project", "https://github.example.com/team/project/issues"},
		{hostingProviderGitLab, "https://gitlab.example.com", ""},
	}
	for _, test := range tests {
		if got := defaultHostingIssuesURL(test.provider, test.projectURL); got != test.want {
			t.Errorf("defaultHostingIssuesURL(%q) = %q, want %q", test.projectURL, got, test.want)
		}
	}
}

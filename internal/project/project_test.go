package project

import "testing"

func TestNormalizeRemote(t *testing.T) {
	cases := map[string]string{
		"https://github.com/org/repo.git":        "github.com/org/repo",
		"https://github.com/org/repo":            "github.com/org/repo",
		"https://user:tok@github.com/org/repo":   "github.com/org/repo",
		"git@github.com:org/repo.git":            "github.com/org/repo",
		"ssh://git@github.com:2222/org/repo.git": "github.com/org/repo",
		"github.com:org/repo.git":                "github.com/org/repo",
		"https://gitlab.com/group/sub/repo.git":  "gitlab.com/group/sub/repo",
		"https://gitlab.com/group/sub/a/b/repo":  "gitlab.com/group/sub/a",
		"":                                       "",
		"   ":                                    "",
		"/local/path/only":                       "",
	}
	for in, want := range cases {
		if got := NormalizeRemote(in); got != want {
			t.Errorf("NormalizeRemote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeRemoteCaseFoldsHost(t *testing.T) {
	if got := NormalizeRemote("https://GitHub.com/Org/Repo.git"); got != "github.com/org/repo" {
		t.Errorf("host not lowercased: %q", got)
	}
}

func TestEscapePath(t *testing.T) {
	// Claude Code convention: separators, dots and colons all become dashes,
	// so the Windows drive colon produces the familiar double dash.
	if got := EscapePath(`C:\Users\me\my.repo`); got != "C--Users-me-my-repo" {
		t.Errorf("EscapePath win = %q", got)
	}
	if got := EscapePath("/home/me/repo"); got != "home-me-repo" {
		t.Errorf("EscapePath unix = %q", got)
	}
	if EscapePath("") != "" {
		t.Error("empty input should stay empty")
	}
}

func TestResolveDirFallback(t *testing.T) {
	// The amh repo itself has no git remote in this workspace, so resolve
	// must fall back to the escaped path rather than erroring.
	got := ResolveDir(".")
	if got == "" {
		t.Fatal("fallback must never be empty")
	}
	if got == "github.com/org/repo" {
		t.Errorf("unexpected remote key %q for a remoteless repo", got)
	}
}

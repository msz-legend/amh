// Package project normalizes project identity. The canonical project key is
// the normalized git remote ("github.com/org/repo"); it is stable across
// machines and working directories, which is what makes cross-agent sharing
// work. When no git remote exists we fall back to the escaped absolute cwd.
package project

import (
	"fmt"
	"net/url"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// EscapePath converts an absolute directory into Claude Code's project-slug
// convention (path separators, dots and colons become dashes), e.g.
// C:\Users\me\repo -> C--Users-me-repo. Used as the fallback project key.
// The caller is expected to pass an already-absolute path.
func EscapePath(dir string) string {
	if dir == "" {
		return ""
	}
	dir = strings.TrimPrefix(strings.ReplaceAll(dir, "\\", "/"), "/")
	var b strings.Builder
	for _, r := range dir {
		switch r {
		case '/', '.', ' ', '_', ':':
			b.WriteRune('-')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

var (
	reScpLike  = regexp.MustCompile(`^[^/@]+@[^:/]+:`) // git@github.com:org/repo.git
	reHostPath = regexp.MustCompile(`^[A-Za-z0-9.-]+\.[A-Za-z]{2,}[:/][^/]+/.+`)
)

// NormalizeRemote reduces any git remote URL to "host/org/repo".
// Accepts https/ssh/scp-like forms and strips protocol, credentials,
// ports, the ".git" suffix and trailing slashes. Returns "" when the
// input is not recognizable as a hosted remote.
func NormalizeRemote(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	// scp-like: git@github.com:org/repo.git
	if m := reScpLike.FindString(raw); m != "" {
		userhost := strings.TrimSuffix(m, ":") // git@github.com
		if i := strings.LastIndex(userhost, "@"); i >= 0 {
			userhost = userhost[i+1:] // github.com
		}
		rest := strings.TrimPrefix(raw, m) // org/repo.git
		return trimRepoSuffix(userhost, rest)
	}
	// URL with scheme
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil {
			return ""
		}
		host := u.Hostname()
		if host == "" {
			return ""
		}
		path := strings.Trim(u.Path, "/")
		return trimRepoSuffix(host, path)
	}
	// bare host/path, e.g. github.com:org/repo.git or ssh:// stripped already
	if reHostPath.MatchString(raw) {
		host := raw[:strings.IndexAny(raw, ":/")]
		rest := raw[len(host)+1:]
		return trimRepoSuffix(host, rest)
	}
	return ""
}

func trimRepoSuffix(host, path string) string {
	path = strings.Trim(path, "/")
	path = strings.TrimSuffix(path, ".git")
	parts := strings.Split(path, "/")
	// Keep at most host/org/repo; drop nested subpaths beyond repo.
	if len(parts) < 2 {
		return ""
	}
	if len(parts) > 3 {
		parts = parts[:3]
	}
	out := strings.ToLower(host + "/" + strings.Join(parts, "/"))
	return out
}

// ResolveDir returns the normalized project key for a working directory:
// `git remote get-url origin` normalized to host/org/repo, or, on any
// failure (not a repo, no origin, git missing), the escaped absolute cwd path.
func ResolveDir(dir string) string {
	out, err := exec.Command("git", "-C", dir, "remote", "get-url", "origin").
		Output()
	if err == nil {
		if k := NormalizeRemote(string(out)); k != "" {
			return k
		}
	}
	return EscapePath(absPath(dir))
}

func absPath(dir string) string {
	if abs, err := filepath.Abs(dir); err == nil {
		return abs
	}
	return dir
}

// MustResolveDir is ResolveDir with an error for callers that need to
// distinguish fallback from git-detected keys (logging / diagnostics).
func MustResolveDir(dir string) (string, error) {
	out, err := exec.Command("git", "-C", dir, "remote", "get-url", "origin").
		Output()
	if err != nil {
		return EscapePath(absPath(dir)), fmt.Errorf("no git origin in %s: %w", dir, err)
	}
	k := NormalizeRemote(string(out))
	if k == "" {
		return EscapePath(absPath(dir)), fmt.Errorf("unrecognizable remote %q", strings.TrimSpace(string(out)))
	}
	return k, nil
}

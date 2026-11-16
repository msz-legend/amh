// Package secrets detects likely credentials in memory content so amh can
// refuse to store them (PRD §7 anti-poisoning trio: secret rejection).
package secrets

import (
	"regexp"
	"strings"
)

// Finding is one detected secret-like substring.
type Finding struct {
	Kind string // e.g. "aws-access-key", "github-token"
	Rule string // rule name that matched
}

type rule struct {
	name string
	kind string
	re   *regexp.Regexp
}

var rules = []rule{
	{"aws-access-key", "aws", regexp.MustCompile(`(?i)\bAKIA[0-9A-Z]{16}\b`)},
	{"aws-secret", "aws", regexp.MustCompile(`(?i)aws(.{0,20})?secret(.{0,20})?[=:]\s*['"]?[A-Za-z0-9/+=]{30,}`)},
	{"github-token", "github", regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{20,}\b`)},
	{"gitlab-token", "gitlab", regexp.MustCompile(`\bglpat-[A-Za-z0-9_\-]{15,}\b`)},
	{"slack-token", "slack", regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9\-]{10,}\b`)},
	{"openai-key", "api-key", regexp.MustCompile(`\bsk-(proj-)?[A-Za-z0-9_\-]{20,}\b`)},
	{"anthropic-key", "api-key", regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_\-]{20,}\b`)},
	{"google-api-key", "api-key", regexp.MustCompile(`\bAIza[0-9A-Za-z_\-]{30,}\b`)},
	{"private-key-block", "key-file", regexp.MustCompile(`(?s)-----BEGIN (RSA |EC |OPENSSH |PGP |DSA )?PRIVATE KEY-----`)},
	{"jwt", "jwt", regexp.MustCompile(`\beyJ[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}\b`)},
	{"bearer-header", "bearer", regexp.MustCompile(`(?i)\bauthorization['"]?\s*[:=]\s*['"]?bearer\s+[A-Za-z0-9._\-]{15,}`)},
	{"generic-credential", "generic", regexp.MustCompile(`(?i)\b(api[_-]?key|secret[_-]?key|access[_-]?token|client[_-]?secret|password)\b['"]?\s*[:=]\s*['"]?[^\s'"]{8,}`)},
}

var placeholders = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(placeholder|dummy|sample|your[_-]?key|xxx+|changeme)`),
	regexp.MustCompile(`\$\{?[A-Za-z_][A-Za-z0-9_]*\}?`), // env interpolation
	regexp.MustCompile(`(?i)(os\.environ|process\.env|getenv|keyring|vault|1password|secrets?\s+manager)`),
	regexp.MustCompile(`<[^>]+>`), // <YOUR_KEY> style placeholders
}

// Detect returns findings for content. Empty result means clean.
func Detect(content string) []Finding {
	var out []Finding
	for _, r := range rules {
		matches := r.re.FindAllString(content, -1)
		if len(matches) == 0 {
			continue
		}
		// If every match in the content is a placeholder or env reference, skip.
		allPlaceholder := true
		for _, m := range matches {
			if !isPlaceholder(m) {
				allPlaceholder = false
				break
			}
		}
		if allPlaceholder {
			continue
		}
		out = append(out, Finding{Kind: r.kind, Rule: r.name})
	}
	return out
}

func isPlaceholder(s string) bool {
	for _, p := range placeholders {
		if p.MatchString(s) {
			return true
		}
	}
	return false
}

// Clean reports content as storeable when it has no findings.
func Clean(content string) bool {
	return len(Detect(content)) == 0
}

// RedactMask masks the middle of a finding for safe logging.
func RedactMask(s string) string {
	if len(s) <= 8 {
		return strings.Repeat("*", len(s))
	}
	return s[:4] + strings.Repeat("*", len(s)-8) + s[len(s)-4:]
}

package secrets

import (
	"strings"
	"testing"
)

// VP for A4: every sample group below MUST be rejected (拦截), and every
// benign line MUST pass (无误杀).
func TestDetectRejects(t *testing.T) {
	samples := map[string]string{
		"aws-access-key":      "deploy with AKIAIOSFODNN7EXAMPLE key",
		"aws-secret":          "aws_secret_access_key = wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		"github-token":        "use ghp_16CharactersEEEEEEEEEEEEEEE1 for auth",
		"gitlab-token":        "glpat-abcdefjhijklmnopqr",
		"slack-token":         "bot token " + "xoxb-" + strings.Repeat("a", 24), // synthetic token assembled to avoid repository scanners
		"openai-key":          "key sk-proj-abcdefghijklmnopqrstuvwxyz1234",
		"anthropic-key":       "key sk-ant-api03-abcdefghijklmnopqrstuvwxyz",
		"google-api-key":      "AIzaSyA1234567890abcdefghijklmnopqrstuv",
		"private-key-block":   "-----BEGIN RSA PRIVATE KEY-----\nMIIEow",
		"jwt":                 "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U",
		"bearer-header":       `authorization: Bearer abcdef1234567890abcdef`,
		"generic-credential1": `api_key = "a1b2c3d4e5f6g7h8"`,
		"generic-credential2": `"client_secret": "s3cr3tvalue12345"`,
		"generic-credential3": "password: hunter2hunter2!",
	}
	for name, content := range samples {
		if Clean(content) {
			t.Errorf("%s: secret NOT rejected: %q", name, content)
		}
	}
}

func TestDetectAllowsBenign(t *testing.T) {
	benign := []string{
		"set AMH_EXTRACT_API_KEY in .env, never commit it",
		"use pnpm, not npm",
		`config: api_key = os.environ["AMH_KEY"]`,
		"the api key template is api_key=\"xxxxxxxx\"",
		"api_key: ${VAULT_ENTRY} interpolated at runtime",
		"password: changeme",
		"secret_key: placeholder for docs",
		"JWTs are three dot-separated base64url segments",
		"deploy uses AWS role assumption, no static keys",
		"read the token from 1Password, never hardcode",
		"xoxb- style tokens come from Slack admin console",
		"we store only config VALUES, not credentials",
	}
	for _, content := range benign {
		if !Clean(content) {
			t.Errorf("benign content wrongly rejected: %q", content)
		}
	}
}

func TestRedactMask(t *testing.T) {
	if got := RedactMask("AKIAIOSFODNN7EXAMPLE"); got != "AKIA************MPLE" {
		t.Errorf("RedactMask = %q", got)
	}
	if got := RedactMask("short"); got != "*****" {
		t.Errorf("RedactMask short = %q", got)
	}
}

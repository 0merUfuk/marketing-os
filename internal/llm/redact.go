package llm

import (
	"regexp"
	"sort"
	"strings"
)

var commonSecretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._~+/=-]{16,}`),
	regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}\b`),
	regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{20,}\b`),
	regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{20,}\b`),
}

func Redact(input string, explicitSecrets []string) string {
	secrets := append([]string(nil), explicitSecrets...)
	sort.Slice(secrets, func(i, j int) bool { return len(secrets[i]) > len(secrets[j]) })
	redacted := input
	for _, secret := range secrets {
		if len(secret) >= 4 {
			redacted = strings.ReplaceAll(redacted, secret, "[REDACTED]")
		}
	}
	for _, pattern := range commonSecretPatterns {
		redacted = pattern.ReplaceAllString(redacted, "[REDACTED]")
	}
	return redacted
}

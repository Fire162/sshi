package sanitize

import (
	"regexp"
	"strings"
	"unicode"
)

// ansiRegex matches ANSI escape codes (OSC, CSI, and 2-byte escape sequences).
var ansiRegex = regexp.MustCompile(`(?:\x1B\][^\x07\x1B]*(?:\x07|\x1B\\)|\x1B\[[0-?]*[ -/]*[@-~]|\x1B[@-Z\\-_]|[\x80-\x9A\x9C-\x9F])`)

// CleanText removes ANSI escape codes and dangerous control characters from untrusted text
// before rendering it into user-facing status lines, overlays, or logs.
func CleanText(input string, maxLen int) string {
	// 1. Strip ANSI escape sequences
	cleaned := ansiRegex.ReplaceAllString(input, "")

	// 2. Filter non-printable control characters
	var b strings.Builder
	b.Grow(len(cleaned))

	for _, r := range cleaned {
		// Disallow non-printable ASCII and control codes
		if r < 32 || r == 127 {
			if r == '\t' || r == '\n' {
				b.WriteRune(r)
			} else {
				// Replace other control characters (e.g. carriage returns, bell, backspace) with a space
				b.WriteRune(' ')
			}
			continue
		}
		// Disallow unicode non-printable control codes
		if unicode.IsControl(r) {
			continue
		}
		b.WriteRune(r)
	}

	result := b.String()
	if maxLen > 0 && len([]rune(result)) > maxLen {
		runes := []rune(result)
		return string(runes[:maxLen]) + "…"
	}
	return result
}

// CleanSingleLine strips ANSI, control codes, and replaces newlines with spaces.
// Perfect for usernames, status bar notifications, and preview labels.
func CleanSingleLine(input string, maxLen int) string {
	cleaned := CleanText(input, maxLen)
	cleaned = strings.ReplaceAll(cleaned, "\r", "")
	cleaned = strings.ReplaceAll(cleaned, "\n", " ")
	cleaned = strings.ReplaceAll(cleaned, "\t", " ")
	// Collapse multiple spaces
	for strings.Contains(cleaned, "  ") {
		cleaned = strings.ReplaceAll(cleaned, "  ", " ")
	}
	return strings.TrimSpace(cleaned)
}

// ValidateAlias checks if a session alias contains only safe, alphanumeric identifier characters.
func ValidateAlias(alias string) bool {
	if len(alias) == 0 || len(alias) > 64 {
		return false
	}
	for _, r := range alias {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' && r != '_' && r != '.' {
			return false
		}
	}
	return true
}

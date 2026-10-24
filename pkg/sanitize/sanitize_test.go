package sanitize

import (
	"strings"
	"testing"
)

func TestCleanText(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxLen   int
		expected string
	}{
		{
			name:     "plain text",
			input:    "Hello world!",
			maxLen:   0,
			expected: "Hello world!",
		},
		{
			name:     "ansi color codes",
			input:    "\033[31mRed\033[0m and \033[1;32mGreen\033[0m",
			maxLen:   0,
			expected: "Red and Green",
		},
		{
			name:     "osc terminal title attack",
			input:    "Normal\033]0;Injected Title\007Text",
			maxLen:   0,
			expected: "NormalText",
		},
		{
			name:     "cursor reposition and screen clear injection",
			input:    "Safe\033[2J\033[H\033[6nInjected",
			maxLen:   0,
			expected: "SafeInjected",
		},
		{
			name:     "control characters bell and null",
			input:    "A\x00B\x07C\x08D",
			maxLen:   0,
			expected: "A B C D",
		},
		{
			name:     "truncation with unicode",
			input:    "This is a long message that should be truncated",
			maxLen:   10,
			expected: "This is a …",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := CleanText(tc.input, tc.maxLen)
			if got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

func TestCleanSingleLine(t *testing.T) {
	input := "User\r\n\tName\033[31m  here  \033[0m"
	got := CleanSingleLine(input, 50)
	if strings.Contains(got, "\n") || strings.Contains(got, "\r") || strings.Contains(got, "\033") {
		t.Fatalf("CleanSingleLine contains forbidden characters: %q", got)
	}
	if got != "User Name here" {
		t.Fatalf("unexpected cleaned result: %q", got)
	}
}

func TestValidateAlias(t *testing.T) {
	valid := []string{"prod", "dev-server", "test_box", "server.1", "node-01"}
	invalid := []string{"", "../etc/passwd", "prod/sub", "alias with spaces", "alias\x00null", "alias\nline"}

	for _, v := range valid {
		if !ValidateAlias(v) {
			t.Errorf("expected %q to be valid alias", v)
		}
	}

	for _, inv := range invalid {
		if ValidateAlias(inv) {
			t.Errorf("expected %q to be invalid alias", inv)
		}
	}
}

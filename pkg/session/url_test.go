package session

import (
	"testing"
)

func TestParseTarget(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		defaultSrv string
		wantHost   string
		wantSecure bool
		wantID     string
		wantKey    string
		wantPass   string
		wantWS     string
	}{
		{
			name:       "full https url",
			input:      "https://sshx.io/s/hr2hQ8YKuf#lFRwv8c7cZhJRd",
			wantHost:   "sshx.io",
			wantSecure: true,
			wantID:     "hr2hQ8YKuf",
			wantKey:    "lFRwv8c7cZhJRd",
			wantWS:     "wss://sshx.io/api/s/hr2hQ8YKuf",
		},
		{
			name:       "short token format",
			input:      "hr2hQ8YKuf#lFRwv8c7cZhJRd",
			wantHost:   "sshx.io",
			wantSecure: true,
			wantID:     "hr2hQ8YKuf",
			wantKey:    "lFRwv8c7cZhJRd",
			wantWS:     "wss://sshx.io/api/s/hr2hQ8YKuf",
		},
		{
			name:       "token with write password",
			input:      "hr2hQ8YKuf#lFRwv8c7cZhJRd,secret123",
			wantHost:   "sshx.io",
			wantSecure: true,
			wantID:     "hr2hQ8YKuf",
			wantKey:    "lFRwv8c7cZhJRd",
			wantPass:   "secret123",
			wantWS:     "wss://sshx.io/api/s/hr2hQ8YKuf",
		},
		{
			name:       "custom localhost server with http",
			input:      "http://localhost:8051/s/test123#mykey",
			wantHost:   "localhost",
			wantSecure: false,
			wantID:     "test123",
			wantKey:    "mykey",
			wantWS:     "ws://localhost:8051/api/s/test123",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			target, err := ParseTarget(tc.input, tc.defaultSrv)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if target.Host != tc.wantHost {
				t.Errorf("Host: got %q, want %q", target.Host, tc.wantHost)
			}
			if target.Secure != tc.wantSecure {
				t.Errorf("Secure: got %v, want %v", target.Secure, tc.wantSecure)
			}
			if target.ID != tc.wantID {
				t.Errorf("ID: got %q, want %q", target.ID, tc.wantID)
			}
			if target.Key != tc.wantKey {
				t.Errorf("Key: got %q, want %q", target.Key, tc.wantKey)
			}
			if target.WritePassword != tc.wantPass {
				t.Errorf("WritePassword: got %q, want %q", target.WritePassword, tc.wantPass)
			}
			if target.WebSocketURL() != tc.wantWS {
				t.Errorf("WebSocketURL: got %q, want %q", target.WebSocketURL(), tc.wantWS)
			}
		})
	}
}

func TestParseTargetErrors(t *testing.T) {
	invalids := []string{
		"",
		"   ",
		"noslash-nohash",
		"https://sshx.io/s/onlyid", // missing hash key
	}

	for _, inv := range invalids {
		_, err := ParseTarget(inv, "")
		if err == nil {
			t.Errorf("expected error for input %q, got nil", inv)
		}
	}
}

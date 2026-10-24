package session

import (
	"fmt"
	"net/url"
	"strings"
)

// DefaultHost is the standard public relay host for sshx.
const DefaultHost = "sshx.io"

// SessionTarget holds the parsed connection parameters for an sshx session.
type SessionTarget struct {
	Host          string
	Port          string
	Secure        bool
	ID            string
	Key           string
	WritePassword string
	RawInput      string
}

// ParseTarget parses a URL, token, or connection string into a SessionTarget.
//
// Supported formats:
//   - https://sshx.io/s/hr2hQ8YKuf#lFRwv8c7cZhJRd
//   - http://localhost:8051/s/hr2hQ8YKuf#lFRwv8c7cZhJRd
//   - sshx.io/s/hr2hQ8YKuf#lFRwv8c7cZhJRd
//   - hr2hQ8YKuf#lFRwv8c7cZhJRd
//   - hr2hQ8YKuf#lFRwv8c7cZhJRd,writePassword
func ParseTarget(input string, defaultServer string) (*SessionTarget, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, fmt.Errorf("session target cannot be empty")
	}

	target := &SessionTarget{
		Host:     DefaultHost,
		Secure:   true,
		RawInput: input,
	}

	if defaultServer != "" {
		defaultServer = strings.TrimSpace(defaultServer)
		if strings.HasPrefix(defaultServer, "http://") || strings.HasPrefix(defaultServer, "https://") {
			if u, err := url.Parse(defaultServer); err == nil {
				target.Host = u.Hostname()
				target.Port = u.Port()
				target.Secure = (u.Scheme != "http")
			}
		} else {
			target.Host = defaultServer
		}
	}

	// 1. Try parsing as full URL
	hasScheme := strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://") ||
		strings.HasPrefix(input, "ws://") || strings.HasPrefix(input, "wss://")

	var fragment string
	var parseURL *url.URL

	if hasScheme {
		var err error
		parseURL, err = url.Parse(input)
		if err != nil {
			return nil, fmt.Errorf("invalid URL: %w", err)
		}
		target.Host = parseURL.Hostname()
		target.Port = parseURL.Port()
		target.Secure = (parseURL.Scheme == "https" || parseURL.Scheme == "wss")
		fragment = parseURL.Fragment

		// Path should look like /s/<id> or /api/s/<id>
		pathParts := strings.Split(strings.Trim(parseURL.Path, "/"), "/")
		if len(pathParts) >= 2 && (pathParts[0] == "s" || pathParts[0] == "api" && len(pathParts) >= 3) {
			if pathParts[0] == "s" {
				target.ID = pathParts[1]
			} else {
				target.ID = pathParts[2]
			}
		} else if len(pathParts) == 1 && pathParts[0] != "" {
			target.ID = pathParts[0]
		}
	} else if strings.Contains(input, "/s/") {
		// host/s/<id>#key
		parts := strings.SplitN(input, "/s/", 2)
		target.Host = parts[0]
		target.Secure = !strings.HasPrefix(target.Host, "localhost") && !strings.HasPrefix(target.Host, "127.0.0.1")

		rest := parts[1]
		if hashIdx := strings.Index(rest, "#"); hashIdx != -1 {
			target.ID = rest[:hashIdx]
			fragment = rest[hashIdx+1:]
		} else {
			target.ID = rest
		}
	} else {
		// Token format: <id>#<key>
		if hashIdx := strings.Index(input, "#"); hashIdx != -1 {
			target.ID = input[:hashIdx]
			fragment = input[hashIdx+1:]
		} else {
			target.ID = input
		}
	}

	// Parse fragment: <key>[,<writePassword>]
	if fragment != "" {
		if commaIdx := strings.Index(fragment, ","); commaIdx != -1 {
			target.Key = fragment[:commaIdx]
			target.WritePassword = fragment[commaIdx+1:]
		} else {
			target.Key = fragment
		}
	}

	if target.ID == "" {
		return nil, fmt.Errorf("missing session ID in target %q", input)
	}
	if target.Key == "" {
		return nil, fmt.Errorf("missing encryption key (hash fragment) in target %q", input)
	}

	return target, nil
}

// WebSocketURL builds the WebSocket endpoint URL (e.g. wss://sshx.io/api/s/hr2hQ8YKuf).
func (s *SessionTarget) WebSocketURL() string {
	scheme := "wss"
	if !s.Secure {
		scheme = "ws"
	}
	host := s.Host
	if s.Port != "" {
		host = fmt.Sprintf("%s:%s", host, s.Port)
	}
	return fmt.Sprintf("%s://%s/api/s/%s", scheme, host, s.ID)
}

// WebOrigin builds the Origin header URL (e.g. https://sshx.io).
func (s *SessionTarget) WebOrigin() string {
	scheme := "https"
	if !s.Secure {
		scheme = "http"
	}
	host := s.Host
	if s.Port != "" {
		host = fmt.Sprintf("%s:%s", host, s.Port)
	}
	return fmt.Sprintf("%s://%s", scheme, host)
}

// WebURL returns the standard browser web link for the session.
func (s *SessionTarget) WebURL() string {
	scheme := "https"
	if !s.Secure {
		scheme = "http"
	}
	host := s.Host
	if s.Port != "" {
		host = fmt.Sprintf("%s:%s", host, s.Port)
	}
	frag := s.Key
	if s.WritePassword != "" {
		frag = fmt.Sprintf("%s,%s", frag, s.WritePassword)
	}
	return fmt.Sprintf("%s://%s/s/%s#%s", scheme, host, s.ID, frag)
}

// String returns a compact representation of the session target.
func (s *SessionTarget) String() string {
	frag := s.Key
	if s.WritePassword != "" {
		frag = fmt.Sprintf("%s,%s", frag, s.WritePassword)
	}
	if s.Host == DefaultHost {
		return fmt.Sprintf("%s#%s", s.ID, frag)
	}
	return s.WebURL()
}

package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/Fire162/sshi/pkg/client"
)

func TestRenderStatusBar(t *testing.T) {
	state := &StatusBarState{
		SessionName:  "test-user@test-host",
		Tabs:         []uint32{1, 2},
		ActiveTabIdx: 0,
		Users: []client.UserInfo{
			{ID: 1, User: client.WsUser{Name: "alice", CanWrite: true}},
			{ID: 2, User: client.WsUser{Name: "bob", CanWrite: false}},
		},
		MyUID:        1,
		RTTPing:      42,
		Visible:      true,
		PrefixActive: false,
	}

	bar := RenderStatusBar(state, 100)
	if !strings.Contains(bar, "SSHI") {
		t.Errorf("expected bar to contain SSHI brand")
	}
	if !strings.Contains(bar, "1:shell#1*") {
		t.Errorf("expected active tab badge")
	}
	if !strings.Contains(bar, "42ms") {
		t.Errorf("expected ping indicator")
	}
	if !strings.Contains(bar, "2 users") {
		t.Errorf("expected 2 users count")
	}
}

func TestRenderHelpOverlay(t *testing.T) {
	help := RenderHelpOverlay()
	if !strings.Contains(help, "Ctrl+A") || !strings.Contains(help, "Shell & Tab Management") {
		t.Errorf("help overlay missing key sections")
	}
	if !strings.Contains(help, "╭") || !strings.Contains(help, "╰") {
		t.Errorf("help overlay should use rounded box borders")
	}
}

func TestRenderUsersOverlay(t *testing.T) {
	users := []client.UserInfo{
		{ID: 10, User: client.WsUser{Name: "fire", CanWrite: true}},
		{ID: 11, User: client.WsUser{Name: "guest", CanWrite: false}},
	}
	overlay := RenderUsersOverlay(users, 10)
	if !strings.Contains(overlay, "fire") || !strings.Contains(overlay, "(you)") {
		t.Errorf("users overlay missing user or (you) tag")
	}
	if !strings.Contains(overlay, "read-only") {
		t.Errorf("users overlay missing read-only indicator")
	}
}

func TestRenderChatHistoryOverlay(t *testing.T) {
	history := []ChatEntry{
		{UID: 1, Name: "alice", Text: "hello world", Timestamp: time.Now()},
		{UID: 2, Name: "bob", Text: "welcome!", Timestamp: time.Now()},
	}
	overlay := RenderChatHistoryOverlay(history, 1)
	if !strings.Contains(overlay, "alice") || !strings.Contains(overlay, "hello world") {
		t.Errorf("chat overlay missing message content")
	}
}

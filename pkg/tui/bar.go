package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/Fire162/sshi/pkg/client"
	"github.com/Fire162/sshi/pkg/sanitize"
)

// ChatEntry represents a logged room message.
type ChatEntry struct {
	UID       uint32
	Name      string
	Text      string
	Timestamp time.Time
}

// StatusBarState holds the view state needed to render the bottom status line.
type StatusBarState struct {
	SessionName   string
	Tabs          []uint32
	ActiveTabIdx  int
	Users         []client.UserInfo
	MyUID         uint32
	RTTPing       uint64
	ShellPing     uint64
	LastChat      *client.MsgChat
	LastChatTime  time.Time
	PrefixActive  bool
	PromptMode    bool
	PromptTitle   string
	PromptInput   string
	Notification  string
	NotifyTime    time.Time
	Visible       bool
}

// RenderStatusBar produces the ANSI string for the bottom line of the terminal.
func RenderStatusBar(state *StatusBarState, cols int) string {
	if !state.Visible || cols < 20 {
		return ""
	}

	// 1. If in interactive prompt mode (chat input or rename)
	if state.PromptMode {
		title := state.PromptTitle
		if title == "" {
			title = "💬 Chat"
		}
		prefix := fmt.Sprintf("\033[1;97;48;5;27m %s \033[0;97;48;5;236m %s\033[5m_\033[0m", title, state.PromptInput)
		visibleLen := stripANSILen(prefix)
		pad := cols - visibleLen
		if pad > 0 {
			prefix += fmt.Sprintf("\033[48;5;236m%s\033[0m", strings.Repeat(" ", pad))
		}
		return prefix
	}

	// 2. Check for recent notification (within 4 seconds)
	now := time.Now()
	if state.Notification != "" && now.Sub(state.NotifyTime) < 4*time.Second {
		msg := fmt.Sprintf("\033[1;30;48;5;220m ⚡ %s \033[0m", sanitize.CleanSingleLine(state.Notification, cols-6))
		return padANSI(msg, cols, "\033[48;5;220m")
	}

	// 3. Incoming chat banner alert (within 6 seconds)
	if state.LastChat != nil && now.Sub(state.LastChatTime) < 6*time.Second {
		name := sanitize.CleanSingleLine(state.LastChat.Name, 18)
		text := sanitize.CleanSingleLine(state.LastChat.Text, cols-35)
		msg := fmt.Sprintf("\033[1;97;48;5;127m 💬 [%s]: \033[0;97;48;5;53m %s \033[38;5;246m(Ctrl+A t to reply)\033[0m", name, text)
		return padANSI(msg, cols, "\033[48;5;53m")
	}

	// 4. Normal status bar
	var left strings.Builder

	// Mode badge / Prefix indicator
	if state.PrefixActive {
		left.WriteString("\033[1;97;48;5;196m ^A \033[0m")
	} else {
		left.WriteString("\033[1;97;48;5;25m SSHI \033[0m")
	}

	// Tabs list
	if len(state.Tabs) == 0 {
		left.WriteString("\033[38;5;244;48;5;236m [no shells] \033[0m")
	} else {
		for i, tid := range state.Tabs {
			if i == state.ActiveTabIdx {
				left.WriteString(fmt.Sprintf("\033[1;30;48;5;45m %d:shell#%d* \033[0m", i+1, tid))
			} else {
				left.WriteString(fmt.Sprintf("\033[38;5;250;48;5;238m %d:#%d \033[0m", i+1, tid))
			}
		}
	}

	// Right items: Session snippet, Ping, Online count, Help
	var right strings.Builder
	right.WriteString("\033[38;5;250;48;5;236m ")

	if state.SessionName != "" && cols > 90 {
		cleanName := sanitize.CleanSingleLine(state.SessionName, 22)
		right.WriteString(fmt.Sprintf("\033[38;5;244m%s\033[38;5;250m | ", cleanName))
	}

	// Ping latency indicator (color-coded)
	if state.RTTPing > 0 {
		var pingColor string
		if state.RTTPing < 80 {
			pingColor = "\033[38;5;82m" // Green
		} else if state.RTTPing < 160 {
			pingColor = "\033[38;5;220m" // Yellow
		} else {
			pingColor = "\033[38;5;203m" // Red
		}
		right.WriteString(fmt.Sprintf("%s⚡ %dms\033[38;5;250m | ", pingColor, state.RTTPing))
	}

	// Users count
	userCount := len(state.Users)
	if userCount <= 1 {
		right.WriteString("\033[38;5;147m👤 1 user\033[38;5;250m | ")
	} else {
		right.WriteString(fmt.Sprintf("\033[38;5;147m👥 %d users\033[38;5;250m | ", userCount))
	}

	right.WriteString("\033[38;5;244m^A ?\033[0m \033[0m")

	leftPlainLen := stripANSILen(left.String())
	rightPlainLen := stripANSILen(right.String())

	gap := cols - leftPlainLen - rightPlainLen
	if gap < 1 {
		gap = 1
	}

	mid := fmt.Sprintf("\033[48;5;236m%s\033[0m", strings.Repeat(" ", gap))
	return left.String() + mid + right.String()
}

// RenderHelpOverlay formats a modern cheat sheet with rounded borders.
func RenderHelpOverlay() string {
	var b strings.Builder
	b.WriteString("\r\n\033[1;36m╭──────────────────────────────────────────────────────────╮\033[0m\r\n")
	b.WriteString("\033[1;36m│\033[0m \033[1;97m🚀 SSHI Keyboard Shortcuts Reference (Prefix: Ctrl+A)    \033[0m\033[1;36m│\033[0m\r\n")
	b.WriteString("\033[1;36m├──────────────────────────────────────────────────────────┤\033[0m\r\n")
	b.WriteString("\033[1;36m│\033[0m \033[1;33m📑 Shell & Tab Management\033[0m                                 \033[1;36m│\033[0m\r\n")
	b.WriteString("\033[1;36m│\033[0m   \033[1;32mCtrl+A c\033[0m       Create a new shell / tab                \033[1;36m│\033[0m\r\n")
	b.WriteString("\033[1;36m│\033[0m   \033[1;32mCtrl+A n\033[0m       Switch to next tab                      \033[1;36m│\033[0m\r\n")
	b.WriteString("\033[1;36m│\033[0m   \033[1;32mCtrl+A p\033[0m       Switch to previous tab                  \033[1;36m│\033[0m\r\n")
	b.WriteString("\033[1;36m│\033[0m   \033[1;32mCtrl+A 1..9\033[0m    Jump directly to Tab 1-9                \033[1;36m│\033[0m\r\n")
	b.WriteString("\033[1;36m│\033[0m   \033[1;32mCtrl+A x\033[0m       Close current shell / tab               \033[1;36m│\033[0m\r\n")
	b.WriteString("\033[1;36m├──────────────────────────────────────────────────────────┤\033[0m\r\n")
	b.WriteString("\033[1;36m│\033[0m \033[1;33m💬 Collaboration & Chat\033[0m                                   \033[1;36m│\033[0m\r\n")
	b.WriteString("\033[1;36m│\033[0m   \033[1;32mCtrl+A t\033[0m       Broadcast chat message to the room      \033[1;36m│\033[0m\r\n")
	b.WriteString("\033[1;36m│\033[0m   \033[1;32mCtrl+A m\033[0m       View recent chat message history        \033[1;36m│\033[0m\r\n")
	b.WriteString("\033[1;36m│\033[0m   \033[1;32mCtrl+A u\033[0m       View connected participants & perms     \033[1;36m│\033[0m\r\n")
	b.WriteString("\033[1;36m│\033[0m   \033[1;32mCtrl+A N\033[0m       Change your display nickname            \033[1;36m│\033[0m\r\n")
	b.WriteString("\033[1;36m├──────────────────────────────────────────────────────────┤\033[0m\r\n")
	b.WriteString("\033[1;36m│\033[0m \033[1;33m⚙  General Controls\033[0m                                       \033[1;36m│\033[0m\r\n")
	b.WriteString("\033[1;36m│\033[0m   \033[1;32mCtrl+A b\033[0m       Toggle bottom status bar on/off         \033[1;36m│\033[0m\r\n")
	b.WriteString("\033[1;36m│\033[0m   \033[1;32mCtrl+A Ctrl+A\033[0m  Send literal Ctrl+A to terminal         \033[1;36m│\033[0m\r\n")
	b.WriteString("\033[1;36m│\033[0m   \033[1;32mCtrl+A d\033[0m / \033[1;32mq\033[0m   Detach / exit SSHI cleanly              \033[1;36m│\033[0m\r\n")
	b.WriteString("\033[1;36m╰──────────────────────────────────────────────────────────╯\033[0m\r\n")
	b.WriteString("\033[2;37m(Press any key to dismiss this window)\033[0m\r\n")
	return b.String()
}

// RenderUsersOverlay formats a popup list of participants with rounded borders.
func RenderUsersOverlay(users []client.UserInfo, myUID uint32) string {
	var b strings.Builder
	b.WriteString("\r\n\033[1;34m╭──────────────────────────────────────────────────────────╮\033[0m\r\n")
	b.WriteString("\033[1;34m│\033[0m \033[1;97m👥 Connected Room Participants                           \033[0m\033[1;34m│\033[0m\r\n")
	b.WriteString("\033[1;34m├──────────────────────────────────────────────────────────┤\033[0m\r\n")
	if len(users) == 0 {
		b.WriteString("\033[1;34m│\033[0m  (No users online)                                       \033[1;34m│\033[0m\r\n")
	} else {
		for _, u := range users {
			tag := ""
			if u.ID == myUID {
				tag = " \033[1;32m(you)\033[0m"
			}
			perm := "\033[38;5;82mread-write\033[0m"
			if !u.User.CanWrite {
				perm = "\033[38;5;214mread-only\033[0m"
			}
			cleanName := sanitize.CleanSingleLine(u.User.Name, 20)
			line := fmt.Sprintf("  • #%-3d %-20s [%s]%s", u.ID, cleanName, perm, tag)
			plainLen := stripANSILen(line)
			if plainLen < 56 {
				line += strings.Repeat(" ", 56-plainLen)
			}
			b.WriteString(fmt.Sprintf("\033[1;34m│\033[0m%s\033[1;34m│\033[0m\r\n", line))
		}
	}
	b.WriteString("\033[1;34m╰──────────────────────────────────────────────────────────╯\033[0m\r\n")
	b.WriteString("\033[2;37m(Press any key to close)\033[0m\r\n")
	return b.String()
}

// RenderChatHistoryOverlay displays recent room messages in a modal.
func RenderChatHistoryOverlay(history []ChatEntry, myUID uint32) string {
	var b strings.Builder
	b.WriteString("\r\n\033[1;35m╭──────────────────────────────────────────────────────────╮\033[0m\r\n")
	b.WriteString("\033[1;35m│\033[0m \033[1;97m💬 Recent Room Chat History                              \033[0m\033[1;35m│\033[0m\r\n")
	b.WriteString("\033[1;35m├──────────────────────────────────────────────────────────┤\033[0m\r\n")
	if len(history) == 0 {
		b.WriteString("\033[1;35m│\033[0m  (No chat messages yet in this session)                  \033[1;35m│\033[0m\r\n")
		b.WriteString("\033[1;35m│\033[0m  Tip: Press \033[1;33mCtrl+A t\033[0m to send a message to everyone!      \033[1;35m│\033[0m\r\n")
	} else {
		// Show up to 10 latest messages
		start := 0
		if len(history) > 10 {
			start = len(history) - 10
		}
		for _, msg := range history[start:] {
			timeStr := msg.Timestamp.Format("15:04")
			name := sanitize.CleanSingleLine(msg.Name, 14)
			text := sanitize.CleanSingleLine(msg.Text, 34)
			tag := "\033[38;5;141m"
			if msg.UID == myUID {
				tag = "\033[38;5;82m"
			}
			line := fmt.Sprintf("  \033[2;37m%s\033[0m %s%-14s\033[0m: %s", timeStr, tag, name, text)
			plainLen := stripANSILen(line)
			if plainLen < 56 {
				line += strings.Repeat(" ", 56-plainLen)
			}
			b.WriteString(fmt.Sprintf("\033[1;35m│\033[0m%s\033[1;35m│\033[0m\r\n", line))
		}
	}
	b.WriteString("\033[1;35m╰──────────────────────────────────────────────────────────╯\033[0m\r\n")
	b.WriteString("\033[2;37m(Press any key to close)\033[0m\r\n")
	return b.String()
}

func stripANSILen(str string) int {
	clean := sanitize.CleanText(str, 0)
	return len([]rune(clean))
}

func padANSI(msg string, cols int, padStyle string) string {
	plainLen := stripANSILen(msg)
	padLen := cols - plainLen
	if padLen > 0 {
		return msg + padStyle + strings.Repeat(" ", padLen) + "\033[0m"
	}
	return msg
}

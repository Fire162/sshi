package tui

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/Fire162/sshi/pkg/client"
	"github.com/Fire162/sshi/pkg/sanitize"
)

const (
	maxTabBufferSize = 512 * 1024 // 512KB scrollback per shell tab
	maxChatHistory   = 50         // Keep last 50 chat messages
	prefixKey        = 0x01       // Ctrl+A
)

type overlayKind int

const (
	overlayNone overlayKind = iota
	overlayHelp
	overlayUsers
	overlayChatHistory
)

type promptKind int

const (
	promptNone promptKind = iota
	promptChat
	promptRename
)

type tabBuffer struct {
	id     uint32
	buffer *bytes.Buffer
}

// Terminal manages the terminal raw-mode lifecycle and interactive session loop.
type Terminal struct {
	client    *client.Client
	stateMu   sync.Mutex
	tabs      map[uint32]*tabBuffer
	tabOrder  []uint32
	activeSID uint32
	activeIdx int

	cols int
	rows int

	oldState *term.State
	exitChan chan struct{}

	prefixActive bool
	promptType   promptKind
	promptTitle  string
	promptInput  string
	overlay      overlayKind
	barVisible   bool

	notification string
	notifyTime   time.Time
	lastChat     *client.MsgChat
	lastChatTime time.Time
	chatHistory  []ChatEntry
}

// NewTerminal creates an interactive TUI instance.
func NewTerminal(c *client.Client) *Terminal {
	return &Terminal{
		client:     c,
		tabs:       make(map[uint32]*tabBuffer),
		exitChan:   make(chan struct{}),
		barVisible: true,
	}
}

// Run attaches stdin/stdout to the sshx session in raw terminal mode.
func (t *Terminal) Run() error {
	stdinFd := int(os.Stdin.Fd())
	if !term.IsTerminal(stdinFd) {
		return fmt.Errorf("stdin is not an interactive terminal; use pipe or run command for automation")
	}

	// Capture initial window size
	cols, rows, err := term.GetSize(stdinFd)
	if err != nil {
		cols, rows = 80, 24
	}
	t.cols = cols
	t.rows = rows

	// Put terminal into raw mode
	oldState, err := term.MakeRaw(stdinFd)
	if err != nil {
		return fmt.Errorf("failed to put terminal into raw mode: %w", err)
	}
	t.oldState = oldState

	// Defensively restore terminal even if panic occurs
	defer func() {
		t.restore()
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "\r\n\033[1;31mRecovered from terminal fault: %v\033[0m\r\n", r)
		}
	}()

	// Clear screen, configure scrolling region 1..(rows-1) so bottom line is dedicated to status bar
	t.setupScreen()

	// Catch SIGWINCH for dynamic resizing, and SIGINT/SIGTERM
	sigChan := make(chan os.Signal, 2)
	signal.Notify(sigChan, syscall.SIGWINCH, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigChan)

	go func() {
		for sig := range sigChan {
			switch sig {
			case syscall.SIGWINCH:
				t.handleResize()
			case syscall.SIGINT, syscall.SIGTERM:
				t.Close()
				return
			}
		}
	}()

	// Periodic status bar refresh ticker (updates ping, time, etc.)
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-t.exitChan:
				return
			case <-ticker.C:
				t.stateMu.Lock()
				t.drawStatusBarLocked()
				t.stateMu.Unlock()
			}
		}
	}()

	// Input read loop from stdin
	return t.inputLoop()
}

// Close exits the terminal loop.
func (t *Terminal) Close() {
	select {
	case <-t.exitChan:
		return
	default:
		close(t.exitChan)
	}
	t.restore()
}

func (t *Terminal) restore() {
	if t.oldState != nil {
		// Reset scrolling region to whole window, show cursor, reset styles
		os.Stdout.WriteString("\033[r\033[?25h\033[0m\r\n")
		_ = term.Restore(int(os.Stdin.Fd()), t.oldState)
		t.oldState = nil
	}
}

func (t *Terminal) setupScreen() {
	scrollRows := t.rows - 1
	if scrollRows < 1 {
		scrollRows = 1
	}
	// \033[2J: clear screen, \033[H: cursor home, \033[1;<scrollRows>r: set margins
	fmt.Fprintf(os.Stdout, "\033[2J\033[H\033[1;%dr", scrollRows)
	t.drawStatusBarLocked()
}

func (t *Terminal) handleResize() {
	t.stateMu.Lock()
	defer t.stateMu.Unlock()

	cols, rows, err := term.GetSize(int(os.Stdin.Fd()))
	if err == nil && cols > 0 && rows > 0 {
		t.cols = cols
		t.rows = rows
		scrollRows := rows - 1
		if scrollRows < 1 {
			scrollRows = 1
		}
		fmt.Fprintf(os.Stdout, "\033[1;%dr", scrollRows)

		if t.activeSID != 0 {
			_ = t.client.ResizeShell(t.activeSID, uint16(scrollRows), uint16(cols))
		}
		t.drawStatusBarLocked()
	}
}

// HandleTerminalData is invoked when decrypted terminal bytes arrive from the server.
func (t *Terminal) HandleTerminalData(sid uint32, data []byte) {
	t.stateMu.Lock()
	defer t.stateMu.Unlock()

	tab, ok := t.tabs[sid]
	if !ok {
		tab = &tabBuffer{id: sid, buffer: bytes.NewBuffer(nil)}
		t.tabs[sid] = tab
	}

	// Append to scrollback buffer with pruning
	tab.buffer.Write(data)
	if tab.buffer.Len() > maxTabBufferSize {
		excess := tab.buffer.Len() - maxTabBufferSize
		tab.buffer.Next(excess)
	}

	// If this chunk belongs to the currently visible tab and no modal is active, output directly
	if sid == t.activeSID && t.overlay == overlayNone {
		os.Stdout.Write(data)
		t.drawStatusBarLocked()
	}
}

// HandleShellsUpdated updates tab list when shells are created or closed.
func (t *Terminal) HandleShellsUpdated(shells []client.ShellInfo) {
	t.stateMu.Lock()
	defer t.stateMu.Unlock()

	newOrder := make([]uint32, 0, len(shells))
	activeStillExists := false

	for _, s := range shells {
		newOrder = append(newOrder, s.ID)
		if s.ID == t.activeSID {
			activeStillExists = true
		}
		if _, ok := t.tabs[s.ID]; !ok {
			t.tabs[s.ID] = &tabBuffer{id: s.ID, buffer: bytes.NewBuffer(nil)}
		}
	}

	t.tabOrder = newOrder

	if len(t.tabOrder) > 0 && (!activeStillExists || t.activeSID == 0) {
		t.switchToTabIdxLocked(0)
	} else if len(t.tabOrder) == 0 {
		t.activeSID = 0
		t.activeIdx = 0
	}

	t.drawStatusBarLocked()
}

// HandleChatMessage records an incoming chat alert.
func (t *Terminal) HandleChatMessage(chat client.MsgChat) {
	t.stateMu.Lock()
	defer t.stateMu.Unlock()

	t.lastChat = &chat
	t.lastChatTime = time.Now()

	// Append to chat history
	entry := ChatEntry{
		UID:       chat.UID,
		Name:      chat.Name,
		Text:      chat.Text,
		Timestamp: time.Now(),
	}
	t.chatHistory = append(t.chatHistory, entry)
	if len(t.chatHistory) > maxChatHistory {
		t.chatHistory = t.chatHistory[len(t.chatHistory)-maxChatHistory:]
	}

	t.drawStatusBarLocked()
}

func (t *Terminal) switchToTabIdxLocked(idx int) {
	if idx < 0 || idx >= len(t.tabOrder) {
		return
	}
	t.activeIdx = idx
	t.activeSID = t.tabOrder[idx]
	_ = t.client.SetFocus(&t.activeSID)

	// Send current terminal dimensions to new shell
	scrollRows := t.rows - 1
	if scrollRows < 1 {
		scrollRows = 1
	}
	_ = t.client.ResizeShell(t.activeSID, uint16(scrollRows), uint16(t.cols))

	// Redraw canvas with tab's cached output
	t.redrawCanvasLocked()
}

func (t *Terminal) redrawCanvasLocked() {
	scrollRows := t.rows - 1
	if scrollRows < 1 {
		scrollRows = 1
	}
	fmt.Fprintf(os.Stdout, "\033[2J\033[H\033[1;%dr", scrollRows)

	if tab, ok := t.tabs[t.activeSID]; ok && tab.buffer.Len() > 0 {
		os.Stdout.Write(tab.buffer.Bytes())
	}
	t.drawStatusBarLocked()
}

func (t *Terminal) drawStatusBarLocked() {
	if !t.barVisible || t.rows <= 1 {
		return
	}

	rtt, shellLat := t.client.Latencies()
	state := &StatusBarState{
		SessionName:  t.client.SessionName(),
		Tabs:         t.tabOrder,
		ActiveTabIdx: t.activeIdx,
		Users:        t.client.GetUsers(),
		MyUID:        t.client.UserID(),
		RTTPing:      rtt,
		ShellPing:    shellLat,
		LastChat:     t.lastChat,
		LastChatTime: t.lastChatTime,
		PrefixActive: t.prefixActive,
		PromptMode:   t.promptType != promptNone,
		PromptTitle:  t.promptTitle,
		PromptInput:  t.promptInput,
		Notification: t.notification,
		NotifyTime:   t.notifyTime,
		Visible:      t.barVisible,
	}

	barText := RenderStatusBar(state, t.cols)
	// Save cursor pos (\033[s), move to bottom line (\033[<rows>;1H), write bar, restore cursor (\033[u)
	fmt.Fprintf(os.Stdout, "\033[s\033[%d;1H%s\033[u", t.rows, barText)
}

func (t *Terminal) inputLoop() error {
	buf := make([]byte, 1024)

	for {
		select {
		case <-t.exitChan:
			return nil
		default:
		}

		n, err := os.Stdin.Read(buf)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if n == 0 {
			continue
		}

		t.stateMu.Lock()
		t.processInputSliceLocked(buf[:n])
		t.stateMu.Unlock()
	}
}

func (t *Terminal) processInputSliceLocked(input []byte) {
	for i := 0; i < len(input); i++ {
		b := input[i]

		// 1. Overlay dismissal (help, user list, or chat history)
		if t.overlay != overlayNone {
			t.overlay = overlayNone
			t.redrawCanvasLocked()
			continue
		}

		// 2. Prompt mode handling (Chat input or Rename prompt)
		if t.promptType != promptNone {
			switch b {
			case '\r', '\n':
				text := t.promptInput
				kind := t.promptType
				t.promptType = promptNone
				t.promptInput = ""
				t.promptTitle = ""

				if text != "" {
					if kind == promptChat {
						clean := sanitize.CleanText(text, 1024)
						_ = t.client.SendChat(clean)
						t.notification = "Chat sent"
						t.notifyTime = time.Now()
					} else if kind == promptRename {
						clean := sanitize.CleanSingleLine(text, 32)
						_ = t.client.SetName(clean)
						t.notification = fmt.Sprintf("Nickname updated to: %s", clean)
						t.notifyTime = time.Now()
					}
				}
				t.drawStatusBarLocked()
			case 0x1b: // Escape cancels prompt
				t.promptType = promptNone
				t.promptInput = ""
				t.promptTitle = ""
				t.drawStatusBarLocked()
			case 0x08, 0x7f: // Backspace
				if len(t.promptInput) > 0 {
					t.promptInput = t.promptInput[:len(t.promptInput)-1]
				}
				t.drawStatusBarLocked()
			default:
				if b >= 32 && b <= 126 && len(t.promptInput) < 200 {
					t.promptInput += string(b)
					t.drawStatusBarLocked()
				}
			}
			continue
		}

		// 3. Prefix mode handling (after Ctrl+A pressed)
		if t.prefixActive {
			t.prefixActive = false
			switch b {
			case prefixKey: // Double Ctrl+A sends literal Ctrl+A
				if t.activeSID != 0 {
					_ = t.client.SendInput(t.activeSID, []byte{prefixKey})
				}
			case 'c', 'C': // Create tab
				_ = t.client.CreateShell(0, 0)
				t.notification = "Created new terminal tab"
				t.notifyTime = time.Now()
			case 'n': // Next tab
				if len(t.tabOrder) > 0 {
					next := (t.activeIdx + 1) % len(t.tabOrder)
					t.switchToTabIdxLocked(next)
				}
			case 'p', 'P': // Prev tab
				if len(t.tabOrder) > 0 {
					prev := (t.activeIdx - 1 + len(t.tabOrder)) % len(t.tabOrder)
					t.switchToTabIdxLocked(prev)
				}
			case '1', '2', '3', '4', '5', '6', '7', '8', '9': // Jump to tab 1..9
				targetIdx := int(b - '1')
				if targetIdx < len(t.tabOrder) {
					t.switchToTabIdxLocked(targetIdx)
				}
			case 'x', 'X': // Close active tab
				if t.activeSID != 0 {
					closingID := t.activeSID
					_ = t.client.CloseShell(closingID)
					t.notification = fmt.Sprintf("Closed tab #%d", closingID)
					t.notifyTime = time.Now()
				}
			case 't', 'T': // Open chat prompt
				t.promptType = promptChat
				t.promptTitle = "💬 Broadcast Chat"
				t.promptInput = ""
			case 'N': // Change display nickname
				t.promptType = promptRename
				t.promptTitle = "👤 Set Nickname"
				t.promptInput = ""
			case 'm', 'M': // View chat history modal
				t.overlay = overlayChatHistory
				os.Stdout.WriteString(RenderChatHistoryOverlay(t.chatHistory, t.client.UserID()))
			case 'u', 'U': // Users overlay
				t.overlay = overlayUsers
				os.Stdout.WriteString(RenderUsersOverlay(t.client.GetUsers(), t.client.UserID()))
			case 'h', 'H', '?': // Help overlay
				t.overlay = overlayHelp
				os.Stdout.WriteString(RenderHelpOverlay())
			case 'b', 'B': // Toggle status bar
				t.barVisible = !t.barVisible
				if !t.barVisible {
					fmt.Fprintf(os.Stdout, "\033[1;%dr", t.rows)
				} else {
					fmt.Fprintf(os.Stdout, "\033[1;%dr", t.rows-1)
				}
				t.redrawCanvasLocked()
			case 'd', 'D', 'q', 'Q': // Detach
				t.Close()
				return
			default:
				// Forward unknown hotkey directly
				if t.activeSID != 0 {
					_ = t.client.SendInput(t.activeSID, []byte{b})
				}
			}
			t.drawStatusBarLocked()
			continue
		}

		// 4. Normal terminal input
		if b == prefixKey {
			t.prefixActive = true
			t.drawStatusBarLocked()
			continue
		}

		// Forward keystroke to active shell
		if t.activeSID != 0 {
			_ = t.client.SendInput(t.activeSID, []byte{b})
		}
	}
}

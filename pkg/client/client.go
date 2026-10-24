package client

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/Fire162/sshi/pkg/crypto"
	"github.com/Fire162/sshi/pkg/sanitize"
	"github.com/Fire162/sshi/pkg/session"
)

// Callbacks receives real-time events from the sshx session.
type Callbacks struct {
	OnConnected      func(uid uint32, sessionName string)
	OnTerminalData   func(sid uint32, data []byte)
	OnShellsUpdated  func(shells []ShellInfo)
	OnUsersUpdated   func(users []UserInfo)
	OnChatMessage    func(chat MsgChat)
	OnLatencyUpdated func(rttMs uint64, shellLatencyMs uint64)
	OnError          func(err error)
	OnDisconnect     func(err error)
}

// Client manages the live WebSocket session connection to an sshx server.
type Client struct {
	target    *session.SessionTarget
	encrypt   *crypto.Encrypt
	callbacks Callbacks

	conn       *websocket.Conn
	connMu     sync.Mutex
	closed     atomic.Bool
	stopChan   chan struct{}

	userID      uint32
	sessionName string

	inputOffset uint64
	offsetMu    sync.Mutex

	subscribed   map[uint32]bool
	subMu        sync.Mutex

	shells       []ShellInfo
	shellsMu     sync.RWMutex

	users        map[uint32]WsUser
	usersMu      sync.RWMutex

	shellLatency uint64
	rttLatency   uint64
	latencyMu    sync.RWMutex
}

// NewClient creates a new sshx client instance.
func NewClient(target *session.SessionTarget, cb Callbacks) *Client {
	// Initialize random starting offset for terminal input encryption
	var randBytes [8]byte
	if _, err := rand.Read(randBytes[:]); err != nil {
		binary.BigEndian.PutUint64(randBytes[:], uint64(time.Now().UnixNano()))
	}
	initialOffset := binary.BigEndian.Uint64(randBytes[:])

	return &Client{
		target:      target,
		encrypt:     crypto.NewEncrypt(target.Key),
		callbacks:   cb,
		stopChan:    make(chan struct{}),
		inputOffset: initialOffset,
		subscribed:  make(map[uint32]bool),
		users:       make(map[uint32]WsUser),
	}
}

// Connect opens the WebSocket connection, executes the handshake and starts message loops.
func (c *Client) Connect(ctx context.Context, displayName string) error {
	header := http.Header{}
	header.Set("Origin", c.target.WebOrigin())

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.DialContext(ctx, c.target.WebSocketURL(), header)
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", c.target.WebSocketURL(), err)
	}
	c.conn = conn

	// 1. Await Hello message
	_, helloData, err := conn.ReadMessage()
	if err != nil {
		conn.Close()
		return fmt.Errorf("failed to receive hello message: %w", err)
	}

	helloMsg, err := DecodeServerMessage(helloData)
	if err != nil || helloMsg.Hello == nil {
		conn.Close()
		return fmt.Errorf("invalid hello response from server: %w", err)
	}

	c.userID = helloMsg.Hello.UID
	c.sessionName = helloMsg.Hello.Name

	// 2. Send Authenticate message
	zeros := c.encrypt.Zeros()
	var writeZeros []byte
	if c.target.WritePassword != "" {
		writeZeros = crypto.NewEncrypt(c.target.WritePassword).Zeros()
	}

	authBytes, err := EncodeAuthenticate(zeros, writeZeros)
	if err != nil {
		conn.Close()
		return fmt.Errorf("failed to encode authenticate: %w", err)
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, authBytes); err != nil {
		conn.Close()
		return fmt.Errorf("failed to send authenticate: %w", err)
	}

	// 3. Set custom user display name if provided
	if displayName != "" {
		nameBytes, _ := EncodeSetName(displayName)
		_ = conn.WriteMessage(websocket.BinaryMessage, nameBytes)
	}

	if c.callbacks.OnConnected != nil {
		c.callbacks.OnConnected(c.userID, c.sessionName)
	}

	// Start reader and ping routines
	go c.readLoop()
	go c.pingLoop()

	return nil
}

// Close gracefully closes the connection.
func (c *Client) Close() error {
	if c.closed.Swap(true) {
		return nil
	}
	close(c.stopChan)

	c.connMu.Lock()
	defer c.connMu.Unlock()
	if c.conn != nil {
		// Send WebSocket close frame
		_ = c.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "bye"))
		return c.conn.Close()
	}
	return nil
}

// readLoop processes incoming server messages until disconnect.
func (c *Client) readLoop() {
	var readErr error
	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			readErr = err
			break
		}

		msg, err := DecodeServerMessage(data)
		if err != nil {
			if c.callbacks.OnError != nil {
				c.callbacks.OnError(fmt.Errorf("error decoding server message: %w", err))
			}
			continue
		}

		c.handleServerMessage(msg)
	}

	c.Close()
	if c.callbacks.OnDisconnect != nil && !errors.Is(readErr, websocket.ErrCloseSent) {
		c.callbacks.OnDisconnect(readErr)
	}
}

// handleServerMessage dispatches parsed server events.
func (c *Client) handleServerMessage(msg *ServerMessage) {
	if msg.InvalidAuth {
		if c.callbacks.OnError != nil {
			c.callbacks.OnError(fmt.Errorf("invalid authentication key"))
		}
		c.Close()
		return
	}

	if msg.Users != nil {
		c.usersMu.Lock()
		c.users = make(map[uint32]WsUser)
		// Limit to 512 users to prevent memory exhaustion DoS
		limit := len(msg.Users)
		if limit > 512 {
			limit = 512
		}
		for i := 0; i < limit; i++ {
			u := msg.Users[i]
			u.User.Name = sanitize.CleanSingleLine(u.User.Name, 48)
			c.users[u.ID] = u.User
		}
		userList := c.getUsersSnapshot()
		c.usersMu.Unlock()

		if c.callbacks.OnUsersUpdated != nil {
			c.callbacks.OnUsersUpdated(userList)
		}
	}

	if msg.UserDiff != nil {
		c.usersMu.Lock()
		if msg.UserDiff.User == nil {
			delete(c.users, msg.UserDiff.UID)
		} else if len(c.users) < 512 {
			u := *msg.UserDiff.User
			u.Name = sanitize.CleanSingleLine(u.Name, 48)
			c.users[msg.UserDiff.UID] = u
		}
		userList := c.getUsersSnapshot()
		c.usersMu.Unlock()

		if c.callbacks.OnUsersUpdated != nil {
			c.callbacks.OnUsersUpdated(userList)
		}
	}

	if msg.Shells != nil {
		shells := msg.Shells
		if len(shells) > 64 {
			shells = shells[:64]
		}
		c.shellsMu.Lock()
		c.shells = shells
		c.shellsMu.Unlock()

		// Auto-subscribe to all new shells
		var newSubs []uint32
		c.subMu.Lock()
		for _, shell := range shells {
			if !c.subscribed[shell.ID] {
				c.subscribed[shell.ID] = true
				newSubs = append(newSubs, shell.ID)
			}
		}
		c.subMu.Unlock()

		for _, sid := range newSubs {
			_ = c.Subscribe(sid, 0)
		}

		if c.callbacks.OnShellsUpdated != nil {
			c.callbacks.OnShellsUpdated(shells)
		}
	}

	if msg.Chunks != nil {
		sid := msg.Chunks.SID
		seq := msg.Chunks.SeqNum
		streamNum := 0x100000000 | uint64(sid)

		for _, chunk := range msg.Chunks.Chunks {
			// Enforce max 8MB per chunk to prevent memory exhaustion
			if len(chunk) > 8*1024*1024 {
				continue
			}
			decrypted := c.encrypt.Segment(streamNum, seq, chunk)
			seq += uint64(len(chunk))
			if c.callbacks.OnTerminalData != nil {
				c.callbacks.OnTerminalData(sid, decrypted)
			}
		}
	}

	if msg.Hear != nil {
		hear := *msg.Hear
		hear.Name = sanitize.CleanSingleLine(hear.Name, 48)
		hear.Text = sanitize.CleanText(hear.Text, 2048)
		if c.callbacks.OnChatMessage != nil {
			c.callbacks.OnChatMessage(hear)
		}
	}

	if msg.ShellLatency != nil {
		c.latencyMu.Lock()
		c.shellLatency = *msg.ShellLatency
		rtt := c.rttLatency
		c.latencyMu.Unlock()

		if c.callbacks.OnLatencyUpdated != nil {
			c.callbacks.OnLatencyUpdated(rtt, *msg.ShellLatency)
		}
	}

	if msg.Pong != nil {
		now := uint64(time.Now().UnixMilli())
		rtt := uint64(0)
		if now >= *msg.Pong {
			rtt = now - *msg.Pong
		}
		c.latencyMu.Lock()
		c.rttLatency = rtt
		shellLat := c.shellLatency
		c.latencyMu.Unlock()

		if c.callbacks.OnLatencyUpdated != nil {
			c.callbacks.OnLatencyUpdated(rtt, shellLat)
		}
	}

	if msg.Error != nil {
		if c.callbacks.OnError != nil {
			c.callbacks.OnError(fmt.Errorf("server: %s", *msg.Error))
		}
	}
}

// pingLoop sends heartbeat pings every 2 seconds.
func (c *Client) pingLoop() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopChan:
			return
		case <-ticker.C:
			ts := uint64(time.Now().UnixMilli())
			data, err := EncodePing(ts)
			if err == nil {
				_ = c.writeBinary(data)
			}
		}
	}
}

// SendInput encrypts keystrokes and transmits them to shell ID sid.
func (c *Client) SendInput(sid uint32, data []byte) error {
	if len(data) == 0 {
		return nil
	}

	c.offsetMu.Lock()
	offset := c.inputOffset
	c.inputOffset += uint64(len(data))
	c.offsetMu.Unlock()

	encrypted := c.encrypt.Segment(0x200000000, offset, data)
	payload, err := EncodeData(sid, encrypted, offset)
	if err != nil {
		return err
	}
	return c.writeBinary(payload)
}

// CreateShell requests creating a new terminal shell.
func (c *Client) CreateShell(x, y int32) error {
	payload, err := EncodeCreate(x, y)
	if err != nil {
		return err
	}
	return c.writeBinary(payload)
}

// CloseShell requests closing a specific terminal shell.
func (c *Client) CloseShell(sid uint32) error {
	payload, err := EncodeClose(sid)
	if err != nil {
		return err
	}
	return c.writeBinary(payload)
}

// ResizeShell updates the terminal rows and columns for a shell.
func (c *Client) ResizeShell(sid uint32, rows, cols uint16) error {
	payload, err := EncodeMove(sid, &WsWinsize{
		Rows: rows,
		Cols: cols,
	})
	if err != nil {
		return err
	}
	return c.writeBinary(payload)
}

// SetFocus notifies the server of the currently focused shell.
func (c *Client) SetFocus(sid *uint32) error {
	payload, err := EncodeSetFocus(sid)
	if err != nil {
		return err
	}
	return c.writeBinary(payload)
}

// SetName updates the participant's display nickname in the session.
func (c *Client) SetName(name string) error {
	payload, err := EncodeSetName(name)
	if err != nil {
		return err
	}
	return c.writeBinary(payload)
}

// SendChat broadcasts a chat message to everyone in the session.
func (c *Client) SendChat(msg string) error {
	payload, err := EncodeChat(msg)
	if err != nil {
		return err
	}
	return c.writeBinary(payload)
}

// Subscribe requests terminal data stream for shell ID sid starting from chunkNum.
func (c *Client) Subscribe(sid uint32, chunkNum uint64) error {
	payload, err := EncodeSubscribe(sid, chunkNum)
	if err != nil {
		return err
	}
	return c.writeBinary(payload)
}

// writeBinary safely writes a binary message to the WebSocket.
func (c *Client) writeBinary(data []byte) error {
	if c.closed.Load() {
		return errors.New("connection closed")
	}
	c.connMu.Lock()
	defer c.connMu.Unlock()
	if c.conn == nil {
		return errors.New("not connected")
	}
	return c.conn.WriteMessage(websocket.BinaryMessage, data)
}

// GetShells returns a snapshot of active shells.
func (c *Client) GetShells() []ShellInfo {
	c.shellsMu.RLock()
	defer c.shellsMu.RUnlock()
	res := make([]ShellInfo, len(c.shells))
	copy(res, c.shells)
	return res
}

// GetUsers returns a snapshot of connected participants.
func (c *Client) GetUsers() []UserInfo {
	c.usersMu.RLock()
	defer c.usersMu.RUnlock()
	return c.getUsersSnapshot()
}

func (c *Client) getUsersSnapshot() []UserInfo {
	res := make([]UserInfo, 0, len(c.users))
	for id, u := range c.users {
		res = append(res, UserInfo{ID: id, User: u})
	}
	return res
}

// UserID returns client's assigned numeric user ID.
func (c *Client) UserID() uint32 {
	return c.userID
}

// SessionName returns the session's name (user@hostname).
func (c *Client) SessionName() string {
	return c.sessionName
}

// Latencies returns (rttMs, shellLatencyMs).
func (c *Client) Latencies() (uint64, uint64) {
	c.latencyMu.RLock()
	defer c.latencyMu.RUnlock()
	return c.rttLatency, c.shellLatency
}

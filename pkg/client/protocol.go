package client

import (
	"fmt"

	"github.com/fxamacker/cbor/v2"
)

// WsWinsize conveys the position and dimensions of a terminal shell window.
type WsWinsize struct {
	X    int32  `cbor:"x"`
	Y    int32  `cbor:"y"`
	Rows uint16 `cbor:"rows"`
	Cols uint16 `cbor:"cols"`
}

// WsUser conveys information about a connected participant in the room.
type WsUser struct {
	Name     string    `cbor:"name"`
	Cursor   *[2]int32 `cbor:"cursor"`
	Focus    *uint32   `cbor:"focus"`
	CanWrite bool      `cbor:"canWrite"`
}

// ShellInfo holds a terminal ID and its window size.
type ShellInfo struct {
	ID   uint32
	Size WsWinsize
}

// UserInfo holds a user ID and participant metadata.
type UserInfo struct {
	ID   uint32
	User WsUser
}

// ServerMessage represents any decoded message received from the sshx server.
type ServerMessage struct {
	Hello        *MsgHello
	InvalidAuth  bool
	Users        []UserInfo
	UserDiff     *MsgUserDiff
	Shells       []ShellInfo
	Chunks       *MsgChunks
	Hear         *MsgChat
	ShellLatency *uint64
	Pong         *uint64
	Error        *string
}

type MsgHello struct {
	UID  uint32
	Name string
}

type MsgUserDiff struct {
	UID  uint32
	User *WsUser // nil if user left
}

type MsgChunks struct {
	SID    uint32
	SeqNum uint64
	Chunks [][]byte
}

type MsgChat struct {
	UID  uint32
	Name string
	Text string
}

// EncodeAuthenticate creates the initial auth message with encrypted zeros.
func EncodeAuthenticate(zeros []byte, writeZeros []byte) ([]byte, error) {
	var wz interface{}
	if writeZeros != nil {
		wz = writeZeros
	}
	m := map[string]interface{}{
		"authenticate": []interface{}{zeros, wz},
	}
	return cbor.Marshal(m)
}

// EncodeSetName sets the client's visible nickname.
func EncodeSetName(name string) ([]byte, error) {
	return cbor.Marshal(map[string]interface{}{
		"setName": name,
	})
}

// EncodeSetFocus sets the active focused shell ID.
func EncodeSetFocus(sid *uint32) ([]byte, error) {
	var val interface{}
	if sid != nil {
		val = *sid
	}
	return cbor.Marshal(map[string]interface{}{
		"setFocus": val,
	})
}

// EncodeCreate creates a new shell window at (x, y).
func EncodeCreate(x, y int32) ([]byte, error) {
	return cbor.Marshal(map[string]interface{}{
		"create": []int32{x, y},
	})
}

// EncodeClose closes a specific shell.
func EncodeClose(sid uint32) ([]byte, error) {
	return cbor.Marshal(map[string]interface{}{
		"close": sid,
	})
}

// EncodeMove moves or resizes a shell window.
func EncodeMove(sid uint32, size *WsWinsize) ([]byte, error) {
	return cbor.Marshal(map[string]interface{}{
		"move": []interface{}{sid, size},
	})
}

// EncodeData sends encrypted user keystrokes / input to a shell.
func EncodeData(sid uint32, encrypted []byte, offset uint64) ([]byte, error) {
	return cbor.Marshal(map[string]interface{}{
		"data": []interface{}{sid, encrypted, offset},
	})
}

// EncodeSubscribe subscribes to a shell starting at chunk sequence chunkNum.
func EncodeSubscribe(sid uint32, chunkNum uint64) ([]byte, error) {
	return cbor.Marshal(map[string]interface{}{
		"subscribe": []interface{}{sid, chunkNum},
	})
}

// EncodeChat sends a chat message to the room.
func EncodeChat(msg string) ([]byte, error) {
	return cbor.Marshal(map[string]interface{}{
		"chat": msg,
	})
}

// EncodePing sends a latency probe timestamp.
func EncodePing(ts uint64) ([]byte, error) {
	return cbor.Marshal(map[string]interface{}{
		"ping": ts,
	})
}

// DecodeServerMessage parses a raw CBOR payload received from the server.
func DecodeServerMessage(data []byte) (*ServerMessage, error) {
	var raw map[string]interface{}
	if err := cbor.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("cbor unmarshal failed: %w", err)
	}

	msg := &ServerMessage{}

	if val, ok := raw["hello"]; ok {
		arr, ok := val.([]interface{})
		if ok && len(arr) >= 2 {
			uid := toUint32(arr[0])
			name, _ := arr[1].(string)
			msg.Hello = &MsgHello{UID: uid, Name: name}
		}
	}

	if _, ok := raw["invalidAuth"]; ok {
		msg.InvalidAuth = true
	}

	if val, ok := raw["users"]; ok {
		if arr, ok := val.([]interface{}); ok {
			for _, item := range arr {
				if pair, ok := item.([]interface{}); ok && len(pair) >= 2 {
					uid := toUint32(pair[0])
					user := parseWsUser(pair[1])
					msg.Users = append(msg.Users, UserInfo{ID: uid, User: user})
				}
			}
		}
	}

	if val, ok := raw["userDiff"]; ok {
		if pair, ok := val.([]interface{}); ok && len(pair) >= 2 {
			uid := toUint32(pair[0])
			var userPtr *WsUser
			if pair[1] != nil {
				u := parseWsUser(pair[1])
				userPtr = &u
			}
			msg.UserDiff = &MsgUserDiff{UID: uid, User: userPtr}
		}
	}

	if val, ok := raw["shells"]; ok {
		if arr, ok := val.([]interface{}); ok {
			for _, item := range arr {
				if pair, ok := item.([]interface{}); ok && len(pair) >= 2 {
					sid := toUint32(pair[0])
					ws := parseWsWinsize(pair[1])
					msg.Shells = append(msg.Shells, ShellInfo{ID: sid, Size: ws})
				}
			}
		}
	}

	if val, ok := raw["chunks"]; ok {
		if arr, ok := val.([]interface{}); ok && len(arr) >= 3 {
			sid := toUint32(arr[0])
			seqnum := toUint64(arr[1])
			var chunks [][]byte
			if rawChunks, ok := arr[2].([]interface{}); ok {
				for _, c := range rawChunks {
					if b, ok := c.([]byte); ok {
						chunks = append(chunks, b)
					}
				}
			}
			msg.Chunks = &MsgChunks{
				SID:    sid,
				SeqNum: seqnum,
				Chunks: chunks,
			}
		}
	}

	if val, ok := raw["hear"]; ok {
		if arr, ok := val.([]interface{}); ok && len(arr) >= 3 {
			uid := toUint32(arr[0])
			name, _ := arr[1].(string)
			text, _ := arr[2].(string)
			msg.Hear = &MsgChat{UID: uid, Name: name, Text: text}
		}
	}

	if val, ok := raw["shellLatency"]; ok {
		lat := toUint64(val)
		msg.ShellLatency = &lat
	}

	if val, ok := raw["pong"]; ok {
		ts := toUint64(val)
		msg.Pong = &ts
	}

	if val, ok := raw["error"]; ok {
		if s, ok := val.(string); ok {
			msg.Error = &s
		}
	}

	return msg, nil
}

func parseWsWinsize(val interface{}) WsWinsize {
	ws := WsWinsize{Rows: 24, Cols: 80}
	if m, ok := val.(map[interface{}]interface{}); ok {
		if x, ok := m["x"]; ok {
			ws.X = int32(toInt64(x))
		}
		if y, ok := m["y"]; ok {
			ws.Y = int32(toInt64(y))
		}
		if r, ok := m["rows"]; ok {
			ws.Rows = uint16(toUint32(r))
		}
		if c, ok := m["cols"]; ok {
			ws.Cols = uint16(toUint32(c))
		}
	} else if m, ok := val.(map[string]interface{}); ok {
		if x, ok := m["x"]; ok {
			ws.X = int32(toInt64(x))
		}
		if y, ok := m["y"]; ok {
			ws.Y = int32(toInt64(y))
		}
		if r, ok := m["rows"]; ok {
			ws.Rows = uint16(toUint32(r))
		}
		if c, ok := m["cols"]; ok {
			ws.Cols = uint16(toUint32(c))
		}
	}
	return ws
}

func parseWsUser(val interface{}) WsUser {
	user := WsUser{CanWrite: true}
	var m map[string]interface{}
	if direct, ok := val.(map[string]interface{}); ok {
		m = direct
	} else if ifaceMap, ok := val.(map[interface{}]interface{}); ok {
		m = make(map[string]interface{})
		for k, v := range ifaceMap {
			if ks, ok := k.(string); ok {
				m[ks] = v
			}
		}
	}

	if m != nil {
		if name, ok := m["name"].(string); ok {
			user.Name = name
		}
		if cw, ok := m["canWrite"].(bool); ok {
			user.CanWrite = cw
		}
		if focus, ok := m["focus"]; ok && focus != nil {
			f := toUint32(focus)
			user.Focus = &f
		}
		if cursor, ok := m["cursor"]; ok && cursor != nil {
			if coords, ok := cursor.([]interface{}); ok && len(coords) >= 2 {
				c := [2]int32{int32(toInt64(coords[0])), int32(toInt64(coords[1]))}
				user.Cursor = &c
			}
		}
	}
	return user
}

func toUint32(v interface{}) uint32 {
	switch n := v.(type) {
	case uint64:
		return uint32(n)
	case int64:
		return uint32(n)
	case uint32:
		return n
	case int32:
		return uint32(n)
	case int:
		return uint32(n)
	case uint:
		return uint32(n)
	case float64:
		return uint32(n)
	default:
		return 0
	}
}

func toUint64(v interface{}) uint64 {
	switch n := v.(type) {
	case uint64:
		return n
	case int64:
		return uint64(n)
	case uint32:
		return uint64(n)
	case int:
		return uint64(n)
	case float64:
		return uint64(n)
	default:
		return 0
	}
}

func toInt64(v interface{}) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case uint64:
		return int64(n)
	case int:
		return int64(n)
	case int32:
		return int64(n)
	case float64:
		return int64(n)
	default:
		return 0
	}
}

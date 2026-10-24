package client

import (
	"bytes"
	"testing"

	"github.com/fxamacker/cbor/v2"
)

func TestEncodeAndDecodeMessages(t *testing.T) {
	// 1. Authenticate encoding
	zeros := []byte{1, 2, 3, 4}
	authBytes, err := EncodeAuthenticate(zeros, nil)
	if err != nil {
		t.Fatalf("EncodeAuthenticate failed: %v", err)
	}
	var raw map[string]interface{}
	if err := cbor.Unmarshal(authBytes, &raw); err != nil {
		t.Fatalf("Unmarshal auth failed: %v", err)
	}
	if _, ok := raw["authenticate"]; !ok {
		t.Fatalf("expected authenticate key in map")
	}

	// 2. Decode Hello
	helloMap := map[string]interface{}{
		"hello": []interface{}{uint64(42), "user@machine"},
	}
	helloBytes, _ := cbor.Marshal(helloMap)
	serverMsg, err := DecodeServerMessage(helloBytes)
	if err != nil {
		t.Fatalf("DecodeServerMessage failed: %v", err)
	}
	if serverMsg.Hello == nil || serverMsg.Hello.UID != 42 || serverMsg.Hello.Name != "user@machine" {
		t.Fatalf("Hello decoded improperly: %+v", serverMsg.Hello)
	}

	// 3. Decode Shells
	shellsMap := map[string]interface{}{
		"shells": []interface{}{
			[]interface{}{uint64(1), map[string]interface{}{"rows": uint64(24), "cols": uint64(80), "x": int64(0), "y": int64(0)}},
			[]interface{}{uint64(2), map[string]interface{}{"rows": uint64(40), "cols": uint64(120), "x": int64(10), "y": int64(20)}},
		},
	}
	shellsBytes, _ := cbor.Marshal(shellsMap)
	serverMsg, err = DecodeServerMessage(shellsBytes)
	if err != nil {
		t.Fatalf("Decode shells failed: %v", err)
	}
	if len(serverMsg.Shells) != 2 {
		t.Fatalf("expected 2 shells, got %d", len(serverMsg.Shells))
	}
	if serverMsg.Shells[0].ID != 1 || serverMsg.Shells[0].Size.Rows != 24 || serverMsg.Shells[0].Size.Cols != 80 {
		t.Fatalf("shell 0 mismatch: %+v", serverMsg.Shells[0])
	}
	if serverMsg.Shells[1].ID != 2 || serverMsg.Shells[1].Size.Rows != 40 || serverMsg.Shells[1].Size.Cols != 120 {
		t.Fatalf("shell 1 mismatch: %+v", serverMsg.Shells[1])
	}

	// 4. Decode Chunks
	chunk1 := []byte("hello ")
	chunk2 := []byte("world")
	chunksMap := map[string]interface{}{
		"chunks": []interface{}{
			uint64(1),
			uint64(100),
			[]interface{}{chunk1, chunk2},
		},
	}
	chunksBytes, _ := cbor.Marshal(chunksMap)
	serverMsg, err = DecodeServerMessage(chunksBytes)
	if err != nil {
		t.Fatalf("Decode chunks failed: %v", err)
	}
	if serverMsg.Chunks == nil || serverMsg.Chunks.SID != 1 || serverMsg.Chunks.SeqNum != 100 {
		t.Fatalf("chunks metadata mismatch: %+v", serverMsg.Chunks)
	}
	if len(serverMsg.Chunks.Chunks) != 2 || !bytes.Equal(serverMsg.Chunks.Chunks[0], chunk1) {
		t.Fatalf("chunks data mismatch: %+v", serverMsg.Chunks.Chunks)
	}

	// 5. Decode Hear (Chat)
	chatMap := map[string]interface{}{
		"hear": []interface{}{uint64(7), "alice", "hey there!"},
	}
	chatBytes, _ := cbor.Marshal(chatMap)
	serverMsg, err = DecodeServerMessage(chatBytes)
	if err != nil {
		t.Fatalf("Decode hear failed: %v", err)
	}
	if serverMsg.Hear == nil || serverMsg.Hear.UID != 7 || serverMsg.Hear.Name != "alice" || serverMsg.Hear.Text != "hey there!" {
		t.Fatalf("hear message mismatch: %+v", serverMsg.Hear)
	}
}

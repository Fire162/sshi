package crypto

import (
	"bytes"
	"testing"
)

func TestMakeEncrypt(t *testing.T) {
	encrypt := NewEncrypt("test")
	defer encrypt.Destroy()

	expected := []byte{198, 3, 249, 238, 65, 10, 224, 98, 253, 73, 148, 1, 138, 3, 108, 143}
	zeros := encrypt.Zeros()

	if !bytes.Equal(zeros, expected) {
		t.Fatalf("Zeros mismatch:\n  got:  %v\n  want: %v", zeros, expected)
	}
}

func TestRoundtripCTR(t *testing.T) {
	encrypt := NewEncrypt("this is a test key")
	defer encrypt.Destroy()
	data := []byte("hello world")

	encrypted := encrypt.Segment(1, 0, data)
	if len(encrypted) != len(data) {
		t.Fatalf("length mismatch: got %d, want %d", len(encrypted), len(data))
	}

	decrypted := encrypt.Segment(1, 0, encrypted)
	if !bytes.Equal(decrypted, data) {
		t.Fatalf("decrypted data does not match original: got %q, want %q", decrypted, data)
	}
}

func TestMatchesOffset(t *testing.T) {
	encrypt := NewEncrypt("this is a test key")
	defer encrypt.Destroy()
	data := []byte("1st block.(16B)|2nd block......|3rd block")

	encrypted := encrypt.Segment(1, 0, data)
	if len(encrypted) != len(data) {
		t.Fatalf("length mismatch: got %d, want %d", len(encrypted), len(data))
	}

	for i := 1; i < len(data); i++ {
		encryptedSuffix := encrypt.Segment(1, uint64(i), data[i:])
		if !bytes.Equal(encryptedSuffix, encrypted[i:]) {
			t.Fatalf("offset %d suffix mismatch", i)
		}
	}
}

func TestZeroStreamNumPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic on streamNum=0, but did not panic")
		}
	}()

	encrypt := NewEncrypt("test")
	defer encrypt.Destroy()
	encrypt.Segment(0, 0, []byte("fail"))
}

func TestDestroy(t *testing.T) {
	encrypt := NewEncrypt("test")
	encrypt.Destroy()
	for _, b := range encrypt.aesKey {
		if b != 0 {
			t.Errorf("expected zeroed byte, got %d", b)
		}
	}
}

func TestConstantTimeCompare(t *testing.T) {
	a := []byte("secret")
	b := []byte("secret")
	c := []byte("wrong!")

	if !ConstantTimeCompare(a, b) {
		t.Errorf("expected match")
	}
	if ConstantTimeCompare(a, c) {
		t.Errorf("expected mismatch")
	}
}

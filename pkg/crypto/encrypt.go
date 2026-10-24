package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/subtle"
	"encoding/binary"
	"fmt"

	"golang.org/x/crypto/argon2"
)

// Salt is the public KDF salt used by sshx to derive 128-bit AES keys from session tokens.
const Salt = "This is a non-random salt for sshx.io, since we want to stretch the security of 83-bit keys!"

// Encrypt handles end-to-end encryption/decryption of terminal streams.
// In CTR mode, encryption and decryption are mathematically symmetric.
type Encrypt struct {
	aesKey []byte
	block  cipher.Block
}

// NewEncrypt derives a 16-byte AES-128 key using Argon2id with sshx parameters.
func NewEncrypt(key string) *Encrypt {
	// Argon2id parameters matching sshx:
	// mem: 19 * 1024 KiB, time: 2 iterations, parallelism: 1, keyLen: 16 bytes
	aesKey := argon2.IDKey([]byte(key), []byte(Salt), 2, 19*1024, 1, 16)
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		panic(fmt.Sprintf("failed to create AES cipher: %v", err))
	}
	return &Encrypt{
		aesKey: aesKey,
		block:  block,
	}
}

// Zeros computes the encrypted zero block used for server-side token authentication.
func (e *Encrypt) Zeros() []byte {
	iv := make([]byte, 16)
	stream := cipher.NewCTR(e.block, iv)
	zeros := make([]byte, 16)
	stream.XORKeyStream(zeros, zeros)
	return zeros
}

// Segment encrypts or decrypts a slice of bytes at a specific stream number and byte offset.
//
// Stream numbers:
//   - Terminal output stream: 0x100000000 | sid (shell ID)
//   - Terminal input stream:  0x200000000
//
// Counter IV structure:
//   - Bytes 0..8:  Big-endian 64-bit stream number
//   - Bytes 8..16: Big-endian 64-bit block index (offset / 16)
//
// Seeking:
//   - If offset % 16 > 0, the keystream is advanced by padBytes to match the stream alignment.
func (e *Encrypt) Segment(streamNum uint64, offset uint64, data []byte) []byte {
	if streamNum == 0 {
		panic("stream number must be nonzero")
	}
	if len(data) == 0 {
		return []byte{}
	}

	blockNum := offset / 16
	padBytes := offset % 16

	iv := make([]byte, 16)
	binary.BigEndian.PutUint64(iv[0:8], streamNum)
	binary.BigEndian.PutUint64(iv[8:16], blockNum)

	stream := cipher.NewCTR(e.block, iv)

	if padBytes > 0 {
		var discard [16]byte
		stream.XORKeyStream(discard[:padBytes], discard[:padBytes])
	}

	out := make([]byte, len(data))
	stream.XORKeyStream(out, data)
	return out
}

// Destroy zeros out sensitive cryptographic key material in memory.
func (e *Encrypt) Destroy() {
	if len(e.aesKey) > 0 {
		for i := range e.aesKey {
			e.aesKey[i] = 0
		}
	}
}

// ConstantTimeCompare verifies two byte blocks in constant time to prevent side-channel timing attacks.
func ConstantTimeCompare(a, b []byte) bool {
	return subtle.ConstantTimeCompare(a, b) == 1
}

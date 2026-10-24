import os
import struct
import argon2.low_level
from cryptography.hazmat.primitives.ciphers import Cipher, algorithms, modes

# KDF salt used by sshx to derive 128-bit AES keys from session tokens.
SALT = b"This is a non-random salt for sshx.io, since we want to stretch the security of 83-bit keys!"

class Encrypt:
    """End-to-end encryption engine for sshx terminal streams."""

    def __init__(self, key: str | bytes):
        if isinstance(key, str):
            key = key.encode("utf-8")
        
        # Derive 16-byte AES-128 key using Argon2id matching sshx parameters:
        # mem: 19 * 1024 KiB, time: 2 iterations, parallelism: 1, keyLen: 16 bytes
        self.aes_key = argon2.low_level.hash_secret_raw(
            secret=key,
            salt=SALT,
            time_cost=2,
            memory_cost=19 * 1024,
            parallelism=1,
            hash_len=16,
            type=argon2.low_level.Type.ID,
        )

    def zeros(self) -> bytes:
        """Compute the encrypted zero block used for server-side token authentication."""
        iv = b"\x00" * 16
        cipher = Cipher(algorithms.AES(self.aes_key), modes.CTR(iv))
        encryptor = cipher.encryptor()
        return encryptor.update(b"\x00" * 16)

    def segment(self, stream_num: int, offset: int, data: bytes) -> bytes:
        """Encrypt or decrypt a slice of bytes at a specific stream number and byte offset.
        
        Stream numbers:
          - Terminal output: 0x100000000 | sid (shell ID)
          - Terminal input:  0x200000000
        """
        if stream_num == 0:
            raise ValueError("stream_num must be nonzero")
        if not data:
            return b""

        block_num = offset // 16
        pad_bytes = offset % 16

        # 16-byte IV: 8 bytes stream_num (big-endian), 8 bytes block_num (big-endian)
        iv = struct.pack(">QQ", stream_num, block_num)
        cipher = Cipher(algorithms.AES(self.aes_key), modes.CTR(iv))
        encryptor = cipher.encryptor()

        # Advance keystream if offset is not 16-byte aligned
        if pad_bytes > 0:
            encryptor.update(b"\x00" * pad_bytes)

        return encryptor.update(data)

    def destroy(self):
        """Zero out sensitive key material in memory."""
        if hasattr(self, "aes_key") and self.aes_key:
            # Overwrite bytearray if possible or clear reference
            self.aes_key = b"\x00" * len(self.aes_key)

import unittest
from sshi.crypto import Encrypt
from sshi.url import parse_target
from sshi.protocol import (
    encode_authenticate,
    encode_set_name,
    encode_chat,
    decode_server_message,
)

class TestCrypto(unittest.TestCase):
    def test_make_encrypt(self):
        enc = Encrypt("test")
        expected = bytes([198, 3, 249, 238, 65, 10, 224, 98, 253, 73, 148, 1, 138, 3, 108, 143])
        self.assertEqual(enc.zeros(), expected)

    def test_roundtrip_ctr(self):
        enc = Encrypt("this is a test key")
        data = b"hello world"
        encrypted = enc.segment(1, 0, data)
        self.assertEqual(len(encrypted), len(data))
        decrypted = enc.segment(1, 0, encrypted)
        self.assertEqual(decrypted, data)

    def test_matches_offset(self):
        enc = Encrypt("this is a test key")
        data = b"1st block.(16B)|2nd block......|3rd block"
        encrypted = enc.segment(1, 0, data)
        for i in range(1, len(data)):
            suffix = enc.segment(1, i, data[i:])
            self.assertEqual(suffix, encrypted[i:])

class TestURL(unittest.TestCase):
    def test_parse_target(self):
        t1 = parse_target("https://sshx.io/s/hr2hQ8YKuf#lFRwv8c7cZhJRd")
        self.assertEqual(t1.host, "sshx.io")
        self.assertTrue(t1.secure)
        self.assertEqual(t1.session_id, "hr2hQ8YKuf")
        self.assertEqual(t1.key, "lFRwv8c7cZhJRd")
        self.assertEqual(t1.websocket_url, "wss://sshx.io/api/s/hr2hQ8YKuf")

        t2 = parse_target("abc123xyz#pass789,writeSecret")
        self.assertEqual(t2.session_id, "abc123xyz")
        self.assertEqual(t2.key, "pass789")
        self.assertEqual(t2.write_password, "writeSecret")

class TestProtocol(unittest.TestCase):
    def test_encode_and_decode(self):
        auth = encode_authenticate(b"1234", None)
        decoded = decode_server_message(auth)
        self.assertIn("authenticate", decoded)

        chat = encode_chat("hello")
        decoded_chat = decode_server_message(chat)
        self.assertEqual(decoded_chat["chat"], "hello")

if __name__ == "__main__":
    unittest.main()

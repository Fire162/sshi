from dataclasses import dataclass
from typing import Any
import cbor2

@dataclass
class WsWinsize:
    rows: int = 24
    cols: int = 80
    x: int = 0
    y: int = 0

@dataclass
class WsUser:
    name: str
    can_write: bool = True
    focus: int | None = None
    cursor: tuple[int, int] | None = None

@dataclass
class ShellInfo:
    id: int
    size: WsWinsize

@dataclass
class ChatMessage:
    uid: int
    name: str
    text: str

def encode_authenticate(zeros: bytes, write_zeros: bytes | None = None) -> bytes:
    return cbor2.dumps({"authenticate": [zeros, write_zeros]})

def encode_set_name(name: str) -> bytes:
    return cbor2.dumps({"setName": name})

def encode_create(x: int = 0, y: int = 0) -> bytes:
    return cbor2.dumps({"create": [x, y]})

def encode_close(sid: int) -> bytes:
    return cbor2.dumps({"close": sid})

def encode_move(sid: int, rows: int, cols: int, x: int = 0, y: int = 0) -> bytes:
    return cbor2.dumps({
        "move": [
            sid,
            {"rows": rows, "cols": cols, "x": x, "y": y}
        ]
    })

def encode_data(sid: int, encrypted_data: bytes, offset: int) -> bytes:
    return cbor2.dumps({"data": [sid, encrypted_data, offset]})

def encode_subscribe(sid: int, chunk_num: int = 0) -> bytes:
    return cbor2.dumps({"subscribe": [sid, chunk_num]})

def encode_chat(text: str) -> bytes:
    return cbor2.dumps({"chat": text})

def encode_ping(timestamp_ms: int) -> bytes:
    return cbor2.dumps({"ping": timestamp_ms})

def decode_server_message(data: bytes) -> dict[str, Any]:
    """Decode raw binary CBOR payload from sshx-server."""
    return cbor2.loads(data)

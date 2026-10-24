import asyncio
import os
import struct
import time
from typing import Callable, Coroutine, Any
import websockets

from .crypto import Encrypt
from .protocol import (
    encode_authenticate,
    encode_set_name,
    encode_subscribe,
    encode_data,
    encode_create,
    encode_close,
    encode_move,
    encode_chat,
    encode_ping,
    decode_server_message,
    ShellInfo,
    WsWinsize,
    WsUser,
    ChatMessage,
)
from .url import SessionTarget

class SSHXClient:
    """Async WebSocket client for sshx sessions with E2EE."""

    def __init__(
        self,
        target: SessionTarget,
        on_data: Callable[[int, bytes], Any] | None = None,
        on_shells: Callable[[list[ShellInfo]], Any] | None = None,
        on_users: Callable[[list[WsUser]], Any] | None = None,
        on_chat: Callable[[ChatMessage], Any] | None = None,
    ):
        self.target = target
        self.encrypt = Encrypt(target.key)
        self.on_data = on_data
        self.on_shells = on_shells
        self.on_users = on_users
        self.on_chat = on_chat

        self.ws: websockets.WebSocketClientProtocol | None = None
        self.user_id: int | None = None
        self.session_name: str | None = None

        # 64-bit random initial input offset
        self._input_offset = struct.unpack(">Q", os.urandom(8))[0]
        self._offset_lock = asyncio.Lock()

        self.shells: dict[int, ShellInfo] = {}
        self.users: dict[int, WsUser] = {}
        self.subscribed: set[int] = set()

        self._read_task: asyncio.Task | None = None
        self._ping_task: asyncio.Task | None = None
        self._closed = False
        self.rtt_ms: int = 0
        self.shell_latency_ms: int = 0

    async def connect(self, display_name: str = "sshi-py") -> None:
        """Connect to sshx server, authenticate, and start background workers."""
        self.ws = await websockets.connect(
            self.target.websocket_url,
            origin=self.target.origin_url,
            max_size=10 * 1024 * 1024, # 10MB frame limit
        )

        # 1. Await Hello message
        hello_data = await self.ws.recv()
        hello_msg = decode_server_message(hello_data)
        if "hello" not in hello_msg:
            raise ConnectionError(f"Unexpected handshake response: {hello_msg}")

        self.user_id = hello_msg["hello"][0]
        self.session_name = hello_msg["hello"][1]

        # 2. Authenticate
        zeros = self.encrypt.zeros()
        write_zeros = None
        if self.target.write_password:
            write_zeros = Encrypt(self.target.write_password).zeros()

        await self.ws.send(encode_authenticate(zeros, write_zeros))

        # 3. Set display name
        if display_name:
            await self.ws.send(encode_set_name(display_name))

        self._read_task = asyncio.create_task(self._read_loop())
        self._ping_task = asyncio.create_task(self._ping_loop())

    async def close(self) -> None:
        """Gracefully disconnect and cancel tasks."""
        if self._closed:
            return
        self._closed = True

        if self._ping_task:
            self._ping_task.cancel()
        if self._read_task:
            self._read_task.cancel()

        if self.ws:
            await self.ws.close()
        self.encrypt.destroy()

    async def send_input(self, sid: int, data: bytes | str) -> None:
        """Encrypt and transmit keystrokes/data to remote shell."""
        if isinstance(data, str):
            data = data.encode("utf-8")
        if not data or not self.ws:
            return

        async with self._offset_lock:
            offset = self._input_offset
            self._input_offset += len(data)

        # Client input stream is always 0x200000000
        encrypted = self.encrypt.segment(0x200000000, offset, data)
        await self.ws.send(encode_data(sid, encrypted, offset))

    async def subscribe(self, sid: int, chunk_num: int = 0) -> None:
        """Subscribe to a specific shell's output stream."""
        if sid in self.subscribed:
            return
        self.subscribed.add(sid)
        if self.ws:
            await self.ws.send(encode_subscribe(sid, chunk_num))

    async def create_shell(self, x: int = 0, y: int = 0) -> None:
        """Request creating a new remote shell window."""
        if self.ws:
            await self.ws.send(encode_create(x, y))

    async def close_shell(self, sid: int) -> None:
        """Request closing a remote shell."""
        if self.ws:
            await self.ws.send(encode_close(sid))

    async def resize_shell(self, sid: int, rows: int, cols: int) -> None:
        """Notify remote shell of dimension changes."""
        if self.ws:
            await self.ws.send(encode_move(sid, rows, cols))

    async def send_chat(self, text: str) -> None:
        """Broadcast a chat message to everyone in the room."""
        if self.ws:
            await self.ws.send(encode_chat(text))

    async def _read_loop(self) -> None:
        try:
            while not self._closed and self.ws:
                raw = await self.ws.recv()
                msg = decode_server_message(raw)
                await self._handle_message(msg)
        except asyncio.CancelledError:
            pass
        except Exception:
            if not self._closed:
                await self.close()

    async def _handle_message(self, msg: dict[str, Any]) -> None:
        if "invalidAuth" in msg:
            raise PermissionError("Invalid authentication key for sshx session")

        if "shells" in msg:
            new_shells = {}
            for sid, win in msg["shells"]:
                ws = WsWinsize(
                    rows=win.get("rows", 24),
                    cols=win.get("cols", 80),
                    x=win.get("x", 0),
                    y=win.get("y", 0),
                )
                new_shells[sid] = ShellInfo(id=sid, size=ws)
            self.shells = new_shells

            # Auto-subscribe to all active shells
            for sid in self.shells:
                await self.subscribe(sid, 0)

            if self.on_shells:
                res = self.on_shells(list(self.shells.values()))
                if asyncio.iscoroutine(res):
                    await res

        if "chunks" in msg:
            sid, seqnum, chunks = msg["chunks"]
            stream_num = 0x100000000 | sid
            for chunk in chunks:
                decrypted = self.encrypt.segment(stream_num, seqnum, chunk)
                seqnum += len(chunk)
                if self.on_data:
                    res = self.on_data(sid, decrypted)
                    if asyncio.iscoroutine(res):
                        await res

        if "users" in msg:
            for uid, u in msg["users"]:
                self.users[uid] = WsUser(
                    name=u.get("name", f"User {uid}"),
                    can_write=u.get("canWrite", True),
                    focus=u.get("focus"),
                )
            if self.on_users:
                res = self.on_users(list(self.users.values()))
                if asyncio.iscoroutine(res):
                    await res

        if "hear" in msg:
            uid, name, text = msg["hear"]
            chat = ChatMessage(uid=uid, name=name, text=text)
            if self.on_chat:
                res = self.on_chat(chat)
                if asyncio.iscoroutine(res):
                    await res

        if "pong" in msg:
            now = int(time.time() * 1000)
            self.rtt_ms = max(0, now - msg["pong"])

        if "shellLatency" in msg:
            self.shell_latency_ms = msg["shellLatency"]

    async def _ping_loop(self) -> None:
        try:
            while not self._closed and self.ws:
                await asyncio.sleep(2.0)
                ts = int(time.time() * 1000)
                await self.ws.send(encode_ping(ts))
        except asyncio.CancelledError:
            pass

import asyncio
from typing import AsyncIterator
from .client import SSHXClient
from .url import parse_target, SessionTarget
from .protocol import ShellInfo, WsUser, ChatMessage

class SSHISession:
    """High-level async interface for AI agents and scripts to interact with sshx sessions."""

    def __init__(
        self,
        target: str | SessionTarget,
        name: str = "ai-agent",
        default_server: str | None = None,
    ):
        if isinstance(target, str):
            self.target = parse_target(target, default_server=default_server)
        else:
            self.target = target

        self.name = name
        self.output_queues: dict[int, asyncio.Queue[bytes]] = {}
        self._shells_ready = asyncio.Event()

        self.client = SSHXClient(
            target=self.target,
            on_data=self._on_data,
            on_shells=self._on_shells,
        )

    async def __aenter__(self) -> "SSHISession":
        await self.connect()
        return self

    async def __aexit__(self, exc_type, exc_val, exc_tb) -> None:
        await self.close()

    async def connect(self, timeout: float = 10.0) -> None:
        """Connect to the session and wait for at least one active shell."""
        await self.client.connect(display_name=self.name)
        try:
            await asyncio.wait_for(self._shells_ready.wait(), timeout=timeout)
        except asyncio.TimeoutError:
            raise TimeoutError("Timed out waiting for remote shell initialization")

    async def close(self) -> None:
        """Close the session connection."""
        await self.client.close()

    @property
    def default_sid(self) -> int:
        """Return the primary active shell ID (preferring the newest shell)."""
        if not self.client.shells:
            raise RuntimeError("No active shells available in session")
        return max(self.client.shells.keys())

    def _on_data(self, sid: int, data: bytes) -> None:
        if sid not in self.output_queues:
            self.output_queues[sid] = asyncio.Queue()
        self.output_queues[sid].put_nowait(data)

    def _on_shells(self, shells: list[ShellInfo]) -> None:
        if shells and not self._shells_ready.is_set():
            self._shells_ready.set()

    async def run(
        self,
        command: str,
        sid: int | None = None,
        timeout: float = 5.0,
    ) -> str:
        """Execute a command remotely and collect output for `timeout` seconds.
        
        Returns clean UTF-8 string output.
        """
        shell_id = sid if sid is not None else self.default_sid

        # Brief pause to allow initial shell history to populate and drain
        await asyncio.sleep(0.3)

        q = self.output_queues.setdefault(shell_id, asyncio.Queue())
        while not q.empty():
            q.get_nowait()

        cmd = command if command.endswith("\n") else command + "\n"
        await self.client.send_input(shell_id, cmd.encode("utf-8"))

        chunks: list[bytes] = []
        end_time = asyncio.get_event_loop().time() + timeout

        while True:
            remaining = end_time - asyncio.get_event_loop().time()
            if remaining <= 0:
                break
            try:
                chunk = await asyncio.wait_for(q.get(), timeout=min(remaining, 1.0))
                chunks.append(chunk)
            except asyncio.TimeoutError:
                if chunks:
                    break
                continue

        return b"".join(chunks).decode("utf-8", errors="replace")

        return b"".join(chunks).decode("utf-8", errors="replace")

    async def stream(
        self,
        command: str | None = None,
        sid: int | None = None,
        timeout: float = 10.0,
    ) -> AsyncIterator[str]:
        """Stream decrypted terminal output chunks live as they arrive."""
        shell_id = sid if sid is not None else self.default_sid
        q = self.output_queues.setdefault(shell_id, asyncio.Queue())

        if command:
            cmd = command if command.endswith("\n") else command + "\n"
            await self.client.send_input(shell_id, cmd.encode("utf-8"))

        end_time = asyncio.get_event_loop().time() + timeout
        while True:
            remaining = end_time - asyncio.get_event_loop().time()
            if remaining <= 0:
                break
            try:
                chunk = await asyncio.wait_for(q.get(), timeout=min(remaining, 1.0))
                yield chunk.decode("utf-8", errors="replace")
            except asyncio.TimeoutError:
                continue

    async def send_input(self, data: str | bytes, sid: int | None = None) -> None:
        """Send raw input bytes or keystrokes to a shell."""
        shell_id = sid if sid is not None else self.default_sid
        await self.client.send_input(shell_id, data)

    async def chat(self, message: str) -> None:
        """Broadcast a chat message to everyone in the session."""
        await self.client.send_chat(message)

    @property
    def shells(self) -> list[ShellInfo]:
        """Return list of active shells."""
        return list(self.client.shells.values())

    @property
    def users(self) -> list[WsUser]:
        """Return list of connected room participants."""
        return list(self.client.users.values())

    @property
    def latency(self) -> tuple[int, int]:
        """Return (rtt_ms, shell_latency_ms)."""
        return self.client.rtt_ms, self.client.shell_latency_ms

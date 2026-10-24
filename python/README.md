# sshi (Python SDK & Agent Client)

> Async Python 3 client and AI Agent SDK for [sshx.io](https://sshx.io) collaborative terminals.

---

## Installation

```bash
pip install .
# Or install dependencies directly
pip install argon2-cffi cryptography cbor2 websockets
```

---

## Python Async API (For AI Agents)

Connect to any live `sshx` terminal session using async context managers:

```python
import asyncio
from sshi import SSHISession

async def main():
    target = "https://sshx.io/s/hr2hQ8YKuf#lFRwv8c7cZhJRd"

    async with SSHISession(target, name="ai-assistant") as session:
        # Run command and get output
        output = await session.run("uname -a && uptime")
        print("Command output:\n", output)

        # Inspect participants
        for user in session.users:
            print(f"User: {user.name} (can_write={user.can_write})")

        # Broadcast a chat message to everyone in the room
        await session.chat("Hello from Python AI agent!")

asyncio.run(main())
```

---

## Python CLI Usage

```bash
# Run command remotely
python3 -m sshi.cli run "hr2hQ8YKuf#lFRwv8c7cZhJRd" "python3 -c 'print(1337 * 2)'"

# Inspect session in JSON
python3 -m sshi.cli info "hr2hQ8YKuf#lFRwv8c7cZhJRd" --json

# Broadcast chat message
python3 -m sshi.cli chat "hr2hQ8YKuf#lFRwv8c7cZhJRd" "Hello room!"
```

import argparse
import asyncio
import json
import os
import sys
from .url import parse_target
from .session import SSHISession

def main() -> None:
    parser = argparse.ArgumentParser(
        prog="sshi-py",
        description="Python 3 client & automation SDK for sshx.io",
    )
    subparsers = parser.add_subparsers(dest="command", help="Available subcommands")

    # Run command
    run_parser = subparsers.add_parser("run", help="Execute command remotely and print output")
    run_parser.add_argument("target", help="Session URL or ID#key")
    run_parser.add_argument("cmd", help="Command string to execute")
    run_parser.add_argument("--timeout", type=float, default=5.0, help="Output wait timeout in seconds")

    # Info command
    info_parser = subparsers.add_parser("info", help="Inspect session participants and active shells")
    info_parser.add_argument("target", help="Session URL or ID#key")
    info_parser.add_argument("--json", action="store_true", help="Output in JSON format")

    # Chat command
    chat_parser = subparsers.add_parser("chat", help="Send chat message to room")
    chat_parser.add_argument("target", help="Session URL or ID#key")
    chat_parser.add_argument("message", help="Chat message to broadcast")

    args = parser.parse_args()
    if not args.command:
        parser.print_help()
        sys.exit(0)

    try:
        if args.command == "run":
            asyncio.run(cli_run(args.target, args.cmd, args.timeout))
        elif args.command == "info":
            asyncio.run(cli_info(args.target, args.json))
        elif args.command == "chat":
            asyncio.run(cli_chat(args.target, args.message))
    except KeyboardInterrupt:
        sys.exit(0)
    except Exception as e:
        print(f"Error: {e}", file=sys.stderr)
        sys.exit(1)

async def cli_run(target_str: str, command: str, timeout: float) -> None:
    async with SSHISession(target_str, name="sshi-python-runner") as session:
        output = await session.run(command, timeout=timeout)
        print(output, end="")

async def cli_info(target_str: str, as_json: bool) -> None:
    async with SSHISession(target_str, name="sshi-python-info") as session:
        shells = [
            {"id": s.id, "rows": s.size.rows, "cols": s.size.cols}
            for s in session.shells
        ]
        users = [
            {"name": u.name, "can_write": u.can_write}
            for u in session.users
        ]
        rtt, shell_lat = session.latency

        if as_json:
            data = {
                "session_id": session.target.session_id,
                "session_name": session.client.session_name,
                "host": session.target.host,
                "shells": shells,
                "users": users,
                "rtt_ms": rtt,
                "shell_latency_ms": shell_lat,
            }
            print(json.dumps(data, indent=2))
        else:
            print(f"Session Target:   {session.target.web_url}")
            print(f"Session Name:     {session.client.session_name}")
            print(f"Active Shells:    {len(shells)}")
            for s in shells:
                print(f"  • Shell #{s['id']}: {s['cols']}x{s['rows']}")
            print(f"Connected Users:  {len(users)}")
            for u in users:
                print(f"  • {u['name']} (can_write={u['can_write']})")
            if rtt > 0:
                print(f"Latency:          RTT: {rtt}ms, Shell: {shell_lat}ms")

async def cli_chat(target_str: str, message: str) -> None:
    async with SSHISession(target_str, name="sshi-python-chat") as session:
        await session.chat(message)
        print(f"Broadcasted chat message: {message}")

if __name__ == "__main__":
    main()

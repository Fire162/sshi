"""
sshi - Python 3 Async Client & AI Agent SDK for sshx.io collaborative terminals.
"""

from .crypto import Encrypt
from .client import SSHXClient
from .session import SSHISession
from .url import SessionTarget, parse_target
from .protocol import ShellInfo, WsUser, WsWinsize, ChatMessage

__version__ = "0.2.0"
__all__ = [
    "SSHISession",
    "SSHXClient",
    "Encrypt",
    "SessionTarget",
    "parse_target",
    "ShellInfo",
    "WsUser",
    "WsWinsize",
    "ChatMessage",
]

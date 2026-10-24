from dataclasses import dataclass
from urllib.parse import urlparse

DEFAULT_HOST = "sshx.io"

@dataclass
class SessionTarget:
    host: str
    secure: bool
    session_id: str
    key: str
    write_password: str | None = None
    port: int | None = None

    @property
    def websocket_url(self) -> str:
        scheme = "wss" if self.secure else "ws"
        netloc = self.host
        if self.port:
            netloc = f"{netloc}:{self.port}"
        return f"{scheme}://{netloc}/api/s/{self.session_id}"

    @property
    def origin_url(self) -> str:
        scheme = "https" if self.secure else "http"
        netloc = self.host
        if self.port:
            netloc = f"{netloc}:{self.port}"
        return f"{scheme}://{netloc}"

    @property
    def web_url(self) -> str:
        scheme = "https" if self.secure else "http"
        netloc = self.host
        if self.port:
            netloc = f"{netloc}:{self.port}"
        frag = self.key
        if self.write_password:
            frag = f"{frag},{self.write_password}"
        return f"{scheme}://{netloc}/s/{self.session_id}#{frag}"


def parse_target(target_str: str, default_server: str | None = None) -> SessionTarget:
    """Parse session target string (URL, token ID#key, or custom host)."""
    target_str = target_str.strip()
    if not target_str:
        raise ValueError("session target cannot be empty")

    host = DEFAULT_HOST
    secure = True
    port = None

    if default_server:
        ds = default_server.strip()
        if ds.startswith(("http://", "https://")):
            parsed_ds = urlparse(ds)
            host = parsed_ds.hostname or DEFAULT_HOST
            port = parsed_ds.port
            secure = parsed_ds.scheme != "http"
        else:
            host = ds

    fragment = ""
    session_id = ""

    if target_str.startswith(("http://", "https://", "ws://", "wss://")):
        parsed = urlparse(target_str)
        host = parsed.hostname or host
        port = parsed.port
        secure = parsed.scheme in ("https", "wss")
        fragment = parsed.fragment
        path_parts = [p for p in parsed.path.strip("/").split("/") if p]
        if len(path_parts) >= 2 and path_parts[0] in ("s", "api"):
            session_id = path_parts[1] if path_parts[0] == "s" else path_parts[2]
        elif len(path_parts) == 1:
            session_id = path_parts[0]
    elif "/s/" in target_str:
        host_part, rest = target_str.split("/s/", 1)
        host = host_part
        secure = not (host.startswith("localhost") or host.startswith("127.0.0.1"))
        if "#" in rest:
            session_id, fragment = rest.split("#", 1)
        else:
            session_id = rest
    else:
        # Token format: <id>#<key>
        if "#" in target_str:
            session_id, fragment = target_str.split("#", 1)
        else:
            session_id = target_str

    write_password = None
    key = ""
    if fragment:
        if "," in fragment:
            key, write_password = fragment.split(",", 1)
        else:
            key = fragment

    if not session_id:
        raise ValueError(f"missing session ID in target: {target_str}")
    if not key:
        raise ValueError(f"missing encryption key in target: {target_str}")

    return SessionTarget(
        host=host,
        secure=secure,
        session_id=session_id,
        key=key,
        write_password=write_password,
        port=port,
    )

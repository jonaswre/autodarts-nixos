#!/usr/bin/env python3
import hmac
import json
import os
import secrets
import socket
import stat
import threading
import urllib.error
import urllib.request
import uuid
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import parse_qs, urlparse

import qrcode
import qrcode.image.svg


HOST = os.environ.get("AUTODARTS_ONBOARDING_BIND", "0.0.0.0")
PORT = int(os.environ.get("AUTODARTS_ONBOARDING_PORT", "3182"))
BOARD_MANAGER = os.environ.get("AUTODARTS_BOARD_MANAGER_URL", "http://127.0.0.1:3180/api").rstrip("/")
STATE_DIR = Path(os.environ.get("AUTODARTS_ONBOARDING_STATE", "/var/lib/autodarts-onboarding"))
ASSET_DIR = Path(__file__).resolve().parent
TOKEN_FILE = STATE_DIR / "pairing-token"


def board_request(path, method="GET", body=None, timeout=20):
    data = None if body is None else json.dumps(body).encode()
    request = urllib.request.Request(
        f"{BOARD_MANAGER}{path}",
        data=data,
        method=method,
        headers={"Content-Type": "application/json"},
    )
    with urllib.request.urlopen(request, timeout=timeout) as response:
        payload = response.read()
    return json.loads(payload) if payload else None


def configuration_state():
    try:
        config = board_request("/config", timeout=3)
        return bool(config.get("auth", {}).get("board_id"))
    except (OSError, ValueError, urllib.error.URLError):
        return None


def is_configured():
    return configuration_state() is True


def pairing_token():
    STATE_DIR.mkdir(mode=0o700, parents=True, exist_ok=True)
    if TOKEN_FILE.exists():
        return TOKEN_FILE.read_text().strip()
    token = secrets.token_urlsafe(32)
    TOKEN_FILE.write_text(token)
    TOKEN_FILE.chmod(stat.S_IRUSR | stat.S_IWUSR)
    return token


def local_address():
    override = os.environ.get("AUTODARTS_ONBOARDING_ADVERTISED_HOST")
    if override:
        return override
    probe = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    try:
        probe.connect(("1.1.1.1", 80))
        return probe.getsockname()[0]
    except OSError:
        return "autodarts"
    finally:
        probe.close()


def remote_control_url():
    return f"https://{local_address()}:6080/vnc.html?autoconnect=true&show_dot=true&resize=scale"


def camera_paths():
    devices = sorted(board_request("/devices"), key=lambda device: device.get("bus", ""))
    paths = []
    for device in devices:
        formats = device.get("formats") or []
        if formats and formats[0].get("path"):
            paths.append(formats[0]["path"])
    if len(paths) < 3:
        raise ValueError(f"Expected three cameras, found {len(paths)}.")
    return paths[:3]


class State:
    calibration = "idle"
    error = ""
    lock = threading.Lock()


def run_calibration():
    with State.lock:
        State.calibration = "running"
        State.error = ""
    try:
        board_request("/config/calibration/auto?distortion=true", method="POST", timeout=180)
        with State.lock:
            State.calibration = "complete"
    except (OSError, ValueError, urllib.error.URLError) as error:
        with State.lock:
            State.calibration = "failed"
            State.error = str(error)


class Handler(BaseHTTPRequestHandler):
    server_version = "AutodartsOnboarding/1"

    def log_message(self, _format, *_args):
        pass

    def send_bytes(self, status, content_type, payload):
        self.send_response(status)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(payload)))
        self.send_header("Cache-Control", "no-store")
        self.send_header("X-Content-Type-Options", "nosniff")
        self.send_header("X-Frame-Options", "DENY")
        self.send_header(
            "Content-Security-Policy",
            "default-src 'self'; img-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'",
        )
        self.end_headers()
        self.wfile.write(payload)

    def send_json(self, status, value):
        self.send_bytes(status, "application/json", json.dumps(value).encode())

    def send_asset(self, filename):
        payload = (ASSET_DIR / filename).read_bytes()
        self.send_bytes(200, "text/html; charset=utf-8", payload)

    def setup_url(self):
        return f"http://{local_address()}:{PORT}/setup?token={pairing_token()}"

    def require_local_display(self):
        if self.client_address[0] not in ("127.0.0.1", "::1"):
            self.send_json(403, {"error": "Pairing details are only shown on the appliance display."})
            return False
        return True

    def do_GET(self):
        path = urlparse(self.path).path
        if path in ("/", "/device"):
            if not self.require_local_display():
                return
            self.send_asset("device.html")
        elif path == "/setup":
            self.send_asset("setup.html")
        elif path == "/qr.svg":
            if not self.require_local_display():
                return
            image = qrcode.make(self.setup_url(), image_factory=qrcode.image.svg.SvgPathImage)
            from io import BytesIO

            output = BytesIO()
            image.save(output)
            self.send_bytes(200, "image/svg+xml", output.getvalue())
        elif path == "/remote-control-qr.svg":
            if not self.require_local_display():
                return
            image = qrcode.make(remote_control_url(), image_factory=qrcode.image.svg.SvgPathImage)
            from io import BytesIO

            output = BytesIO()
            image.save(output)
            self.send_bytes(200, "image/svg+xml", output.getvalue())
        elif path == "/api/pairing":
            if not self.require_local_display():
                return
            self.send_json(200, {"configured": is_configured(), "url": self.setup_url()})
        elif path == "/api/status":
            configured = configuration_state()
            with State.lock:
                self.send_json(
                    200,
                    {
                        "ready": configured is not None,
                        "configured": configured is True,
                        "calibration": State.calibration,
                        "error": State.error,
                    },
                )
        elif path == "/api/configured":
            configured = configuration_state()
            self.send_bytes(204 if configured is True else 503 if configured is None else 409, "text/plain", b"")
        elif path == "/api/setup-required":
            configured = configuration_state()
            self.send_bytes(204 if configured is False else 503 if configured is None else 409, "text/plain", b"")
        else:
            self.send_json(404, {"error": "Not found."})

    def do_POST(self):
        if urlparse(self.path).path != "/api/setup":
            self.send_json(404, {"error": "Not found."})
            return
        try:
            length = int(self.headers.get("Content-Length", "0"))
            if length < 1 or length > 16_384:
                raise ValueError("Invalid request size.")
            data = json.loads(self.rfile.read(length))
            token = str(data.get("token", ""))
            if not TOKEN_FILE.exists() or not hmac.compare_digest(token, pairing_token()):
                self.send_json(403, {"error": "Pairing code is invalid or has expired."})
                return

            board_id = str(uuid.UUID(str(data.get("board_id", ""))))
            api_key = str(data.get("api_key", ""))
            if not 16 <= len(api_key) <= 2048 or any(character.isspace() for character in api_key):
                raise ValueError("API key format is invalid.")

            cams = camera_paths()
            config = board_request(
                "/config",
                method="PATCH",
                body={
                    "auth": {"board_id": board_id, "api_key": api_key},
                    "cam": {
                        "cams": cams,
                        "width": 1280,
                        "height": 720,
                        "fps": 30,
                        "rotate_180": [False, False, False],
                        "auto_calibrate": True,
                        "auto_calibrate_on_start": True,
                        "auto_distortion": True,
                    },
                },
            )
            if config.get("auth", {}).get("board_id") != board_id:
                raise ValueError("Board Manager did not retain the Board ID.")

            TOKEN_FILE.unlink(missing_ok=True)
            threading.Thread(target=run_calibration, daemon=True).start()
            self.send_json(200, {"ok": True, "cameras": len(cams), "calibration": "running"})
        except (KeyError, OSError, ValueError, json.JSONDecodeError, urllib.error.URLError) as error:
            self.send_json(400, {"error": str(error)})


if __name__ == "__main__":
    ThreadingHTTPServer((HOST, PORT), Handler).serve_forever()

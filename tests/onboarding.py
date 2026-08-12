#!/usr/bin/env python3
import json
import os
import subprocess
import sys
import tempfile
import threading
import time
import urllib.error
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import parse_qs, urlparse


APP = Path(sys.argv[1])
ASSET_DIR = Path(sys.argv[2])
MANAGER_PORT = 18082
PORTAL_PORT = 18083
BOARD_ID = "4dff4c92-3451-450e-9cc4-d163090a156a"
API_KEY = "test-api-key-1234567890"


class ManagerState:
    config = {"auth": {"board_id": "", "api_key": ""}}
    patched = None
    calibrated = threading.Event()


class Manager(BaseHTTPRequestHandler):
    def log_message(self, _format, *_args):
        pass

    def reply(self, status, value=None):
        payload = b"" if value is None else json.dumps(value).encode()
        self.send_response(status)
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def do_GET(self):
        if self.path == "/api/config":
            self.reply(200, ManagerState.config)
        elif self.path == "/api/devices":
            self.reply(
                200,
                [
                    {"bus": "usb-3", "formats": [{"path": "/dev/video4"}]},
                    {"bus": "usb-1", "formats": [{"path": "/dev/video0"}]},
                    {"bus": "usb-2", "formats": [{"path": "/dev/video2"}]},
                ],
            )
        else:
            self.reply(404)

    def do_PATCH(self):
        if self.path != "/api/config":
            self.reply(404)
            return
        length = int(self.headers["Content-Length"])
        ManagerState.patched = json.loads(self.rfile.read(length))
        ManagerState.config = ManagerState.patched
        self.reply(200, ManagerState.config)

    def do_POST(self):
        if self.path == "/api/config/calibration/auto?distortion=true":
            ManagerState.calibrated.set()
            self.reply(204)
        else:
            self.reply(404)


def request(path, method="GET", body=None):
    payload = None if body is None else json.dumps(body).encode()
    req = urllib.request.Request(
        f"http://127.0.0.1:{PORTAL_PORT}{path}",
        data=payload,
        method=method,
        headers={"Content-Type": "application/json"},
    )
    with urllib.request.urlopen(req, timeout=5) as response:
        content = response.read()
        return response.status, response.headers, content


def wait_ready():
    for _ in range(50):
        try:
            status, _, _ = request("/api/setup-required")
            if status == 204:
                return
        except (OSError, urllib.error.URLError):
            time.sleep(0.1)
    raise AssertionError("Onboarding service did not become ready")


with tempfile.TemporaryDirectory() as state_dir:
    manager = ThreadingHTTPServer(("127.0.0.1", MANAGER_PORT), Manager)
    manager_thread = threading.Thread(target=manager.serve_forever, daemon=True)
    manager_thread.start()

    environment = os.environ.copy()
    environment.update(
        {
            "AUTODARTS_ONBOARDING_BIND": "127.0.0.1",
            "AUTODARTS_ONBOARDING_PORT": str(PORTAL_PORT),
            "AUTODARTS_BOARD_MANAGER_URL": f"http://127.0.0.1:{MANAGER_PORT}/api",
            "AUTODARTS_ONBOARDING_STATE": state_dir,
            "AUTODARTS_ONBOARDING_ADVERTISED_HOST": "192.0.2.10",
            "AUTODARTS_ONBOARDING_ASSETS": str(ASSET_DIR),
        }
    )
    portal = subprocess.Popen([str(APP)], env=environment)
    try:
        wait_ready()

        _, _, device_page = request("/device")
        assert b"Finish setup on your phone" in device_page
        _, _, setup_page = request("/setup?token=hidden")
        assert b'id="board-id"' in setup_page
        assert b'id="api-key"' in setup_page
        _, headers, qr = request("/qr.svg")
        assert headers.get_content_type() == "image/svg+xml"
        assert b"<svg" in qr
        _, remote_headers, remote_qr = request("/remote-control-qr.svg")
        assert remote_headers.get_content_type() == "image/svg+xml"
        assert b"<svg" in remote_qr

        _, _, play_status = request("/api/play-capture/status")
        assert json.loads(play_status) == {
            "connected": False,
            "messages": 0,
            "capture": False,
            "events": 0,
        }
        request("/api/play-capture/start", method="POST", body={})
        play_message = {
            "id": "match-secret-id",
            "players": [{"id": "player-secret-id", "name": "Secret Player"}],
            "player": "Scalar Secret Player",
            "topic": "0d23d72f-c84f-4bfe-bd76-c20375c43c07.state",
            "boardName": "Private Board Name",
            "board": "Private Board Name",
            "boards": {BOARD_ID: {"score": 64}},
            "avatarUrl": "https://example.invalid/private-avatar-hash",
            "turns": [
                {
                    "throws": [
                        {
                            "segment": {
                                "name": "T20",
                                "number": 20,
                                "bed": "Triple",
                                "multiplier": 3,
                            }
                        }
                    ]
                }
            ],
            "access_token": "secret-token",
        }
        request(
            "/api/play-event",
            method="POST",
            body={
                "url": f"wss://api.autodarts.io/matches/{BOARD_ID}/events?token=secret-query",
                "data": json.dumps(play_message),
                "transport": "websocket",
            },
        )
        _, _, current_play_state = request("/api/play-state")
        captured = json.loads(current_play_state)
        encoded = json.dumps(captured)
        assert captured["source"].startswith("wss://api.autodarts.io/matches/[id-")
        assert captured["transport"] == "websocket"
        assert captured["message"]["turns"][0]["throws"][0]["segment"]["name"] == "T20"
        assert captured["message"]["board"].startswith("[name-")
        assert "Secret Player" not in encoded
        assert "match-secret-id" not in encoded
        assert "player-secret-id" not in encoded
        assert "secret-token" not in encoded
        assert "secret-query" not in encoded
        assert "0d23d72f-c84f-4bfe-bd76-c20375c43c07" not in encoded
        assert "Private Board Name" not in encoded
        assert "Scalar Secret Player" not in encoded
        assert "private-avatar-hash" not in encoded
        request("/api/play-capture/stop", method="POST", body={})
        assert (Path(state_dir) / "play-events.jsonl").read_text().count("\n") == 1

        _, _, pairing = request("/api/pairing")
        setup_url = json.loads(pairing)["url"]
        token = parse_qs(urlparse(setup_url).query)["token"][0]
        assert setup_url.startswith(f"http://192.0.2.10:{PORTAL_PORT}/setup?")

        _, _, result = request(
            "/api/setup",
            method="POST",
            body={"token": token, "board_id": BOARD_ID, "api_key": API_KEY},
        )
        assert json.loads(result) == {"ok": True, "cameras": 3, "calibration": "running"}
        assert ManagerState.patched["auth"] == {"board_id": BOARD_ID, "api_key": API_KEY}
        assert ManagerState.patched["cam"]["cams"] == ["/dev/video0", "/dev/video2", "/dev/video4"]
        assert ManagerState.patched["cam"]["width"] == 1280
        assert ManagerState.patched["cam"]["height"] == 720
        assert ManagerState.patched["cam"]["fps"] == 30
        assert ManagerState.calibrated.wait(3)

        try:
            request(
                "/api/setup",
                method="POST",
                body={"token": token, "board_id": BOARD_ID, "api_key": API_KEY},
            )
            raise AssertionError("Consumed pairing token was accepted")
        except urllib.error.HTTPError as error:
            assert error.code == 403
    finally:
        portal.terminate()
        portal.wait(timeout=5)
        manager.shutdown()

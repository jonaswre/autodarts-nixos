#!/usr/bin/env python3
import ssl
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request
from pathlib import Path


WEBSOCKIFY, NOVNC_ROOT, OPENSSL = sys.argv[1:]


with tempfile.TemporaryDirectory() as temporary:
    state = Path(temporary)
    certificate = state / "certificate.pem"
    key = state / "key.pem"
    subprocess.run(
        [
            OPENSSL,
            "req",
            "-x509",
            "-newkey",
            "rsa:2048",
            "-nodes",
            "-days",
            "1",
            "-subj",
            "/CN=autodarts",
            "-keyout",
            key,
            "-out",
            certificate,
        ],
        check=True,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )
    proxy = subprocess.Popen(
        [
            WEBSOCKIFY,
            "--ssl-only",
            f"--cert={certificate}",
            f"--key={key}",
            f"--web={NOVNC_ROOT}",
            "127.0.0.1:16080",
            "127.0.0.1:15900",
        ],
    )
    try:
        context = ssl._create_unverified_context()
        for _ in range(50):
            try:
                with urllib.request.urlopen(
                    "https://127.0.0.1:16080/vnc.html", context=context, timeout=2
                ) as response:
                    page = response.read()
                break
            except (OSError, urllib.error.URLError):
                time.sleep(0.1)
        else:
            raise AssertionError("HTTPS noVNC page did not become ready")

        assert b"noVNC" in page
        assert b"noVNC_credentials" in page
        try:
            urllib.request.urlopen("http://127.0.0.1:16080/vnc.html", timeout=2)
            raise AssertionError("Remote control accepted unencrypted HTTP")
        except (OSError, urllib.error.URLError):
            pass
    finally:
        proxy.terminate()
        proxy.wait(timeout=5)

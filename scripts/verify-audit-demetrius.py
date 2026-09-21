#!/usr/bin/env python3
"""Verify the installed audit build without printing credentials or chat logs."""
import hashlib
import json
from pathlib import Path
import re
import subprocess
import sys
import urllib.request


def run(*args):
    return subprocess.run(args, check=True, capture_output=True, text=True, timeout=45).stdout


binary = "/opt/kuromatsu/bin/kuromatsu"
actual = hashlib.sha256(Path(binary).read_bytes()).hexdigest()
if len(sys.argv) != 2 or actual != sys.argv[1]:
    raise RuntimeError("installed checksum mismatch")
print("SHA256", actual)
assert run("systemctl", "is-active", "kuromatsu").strip() == "active"
pid = run("systemctl", "show", "kuromatsu", "-p", "MainPID", "--value").strip()
environment = dict(item.split(b"=", 1) for item in Path(f"/proc/{pid}/environ").read_bytes().split(b"\0") if b"=" in item)
assert environment.get(b"KUROMATSU_CONFIG") == b"/var/lib/kuromatsu/config.json"
assert not Path("/var/lib/kuromatsu/workspace/config.json").exists()
cfg = json.loads(Path("/var/lib/kuromatsu/config.json").read_text())
port = cfg.get("gateway", {}).get("port", 18790)
with urllib.request.urlopen(f"http://127.0.0.1:{port}/ready", timeout=5) as response:
    assert response.status == 200
print("SERVICE active; readiness 200; config explicit; shadow absent")
for command, expected in (
    ("/show model", ["Current Model: Bonsai-1.7B-Q1_0 (Provider: native)"]),
    ("/show config", ["Active config: /var/lib/kuromatsu/config.json",
                      "Default workspace: /var/lib/kuromatsu/workspace"]),
):
    output = run("sudo", "-u", "kuromatsu", "env",
                 "KUROMATSU_HOME=/var/lib/kuromatsu",
                 "KUROMATSU_CONFIG=/var/lib/kuromatsu/config.json",
                 "KUROMATSU_MODELS_DIR=/var/lib/kuromatsu/models",
                 binary, "agent", "-m", command, "-s", "cli:audit-config-20260916")
    output = re.sub(r"\x1b\[[0-9;]*m", "", output)
    for line in expected:
        if line not in output:
            raise RuntimeError(f"verification failed for {command}")
        print(line)
print("VERIFIED")

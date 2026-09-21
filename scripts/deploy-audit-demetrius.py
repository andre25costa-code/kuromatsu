#!/usr/bin/env python3
"""Install a validated audit build on demetrius with private backups and rollback.

Run as root on the target: python3 deploy-audit-demetrius.py STAGED_BINARY SHA256
Does not print configuration contents or credentials.
"""
import hashlib
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import time
import urllib.request


def run(*args):
    return subprocess.run(args, check=True, capture_output=True, text=True).stdout.strip()


def atomic_write(path, data, mode=0o600, uid=0, gid=0):
    path.parent.mkdir(parents=True, exist_ok=True)
    fd, temporary = tempfile.mkstemp(prefix=".audit-", dir=path.parent)
    try:
        os.fchmod(fd, mode)
        os.fchown(fd, uid, gid)
        with os.fdopen(fd, "wb") as output:
            output.write(data)
            output.flush()
            os.fsync(output.fileno())
        os.replace(temporary, path)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def main():
    if os.geteuid() != 0:
        raise RuntimeError("root is required")
    staged, expected = Path(sys.argv[1]), sys.argv[2]
    if hashlib.sha256(staged.read_bytes()).hexdigest() != expected:
        raise RuntimeError("staged binary checksum mismatch")
    staged.chmod(0o755)
    run(str(staged), "version")
    home = Path("/var/lib/kuromatsu")
    config = home / "config.json"
    legacy = home / "workspace/config.json"
    binary = Path("/opt/kuromatsu/bin/kuromatsu")
    dropin = Path("/etc/systemd/system/kuromatsu.service.d/90-config-source.conf")
    cfg = json.loads(config.read_text())
    defaults = cfg["agents"]["defaults"]
    if defaults["workspace"] != str(home / "workspace"):
        raise RuntimeError("unexpected active workspace; refusing an unrelated deployment")
    primary = next(m for m in cfg["model_list"] if m["model_name"] == defaults["model_name"])
    if primary.get("provider") != "native" or primary["model"] != "Bonsai-1.7B-Q1_0":
        raise RuntimeError("unexpected active primary model")
    backup_root = Path("/var/backups/kuromatsu")
    backup_root.mkdir(mode=0o700, parents=True, exist_ok=True)
    prefix = datetime.now(timezone.utc).strftime("audit-%Y%m%d-")
    backup = Path(tempfile.mkdtemp(prefix=prefix, dir=backup_root))
    snapshots = [(config, "active-config.json"), (legacy, "workspace-config.legacy.json"),
                 (binary, "kuromatsu.previous"), (dropin, "config-source.previous.conf")]
    existed = {}
    for source, name in snapshots:
        existed[name] = source.exists()
        if source.exists():
            shutil.copy2(source, backup / name)
    print("BACKUP", backup, flush=True)
    config_stat = config.stat()
    try:
        run("systemctl", "stop", "kuromatsu")
        heartbeat_log = home / "workspace/heartbeat.log"
        print("HEARTBEAT_LOG_OFFSET", heartbeat_log.stat().st_size if heartbeat_log.exists() else 0, flush=True)
        # Keep the legacy default consistent as well as the canonical model_list.
        defaults["provider"] = "native"
        atomic_write(config, (json.dumps(cfg, ensure_ascii=False, indent=2) + "\n").encode(),
                     config_stat.st_mode & 0o777, config_stat.st_uid, config_stat.st_gid)
        if legacy.exists():
            legacy.unlink()  # The exact original is retained in the private backup.
        atomic_write(dropin, b"[Service]\nEnvironment=KUROMATSU_HOME=/var/lib/kuromatsu\nEnvironment=KUROMATSU_CONFIG=/var/lib/kuromatsu/config.json\n", mode=0o644)
        atomic_write(binary, staged.read_bytes(), mode=0o755)
        run("systemctl", "daemon-reload")
        run("systemctl", "start", "kuromatsu")
        port = cfg.get("gateway", {}).get("port", 18790)
        ready = False
        for _ in range(45):
            if run("systemctl", "is-active", "kuromatsu") != "active":
                raise RuntimeError("service failed after deployment")
            try:
                with urllib.request.urlopen(f"http://127.0.0.1:{port}/ready", timeout=2) as response:
                    ready = response.status == 200
            except OSError:
                pass
            if ready:
                break
            time.sleep(1)
        if not ready:
            raise RuntimeError("readiness probe failed")
        print("DEPLOYED", expected, "READY", flush=True)
    except Exception:
        subprocess.run(["systemctl", "stop", "kuromatsu"], capture_output=True)
        for target, name in snapshots:
            if existed[name]:
                shutil.copy2(backup / name, target)
            elif target == dropin and target.exists():
                target.unlink()
        os.chown(config, config_stat.st_uid, config_stat.st_gid)
        if legacy.exists():
            os.chown(legacy, config_stat.st_uid, config_stat.st_gid)
        run("systemctl", "daemon-reload")
        run("systemctl", "reset-failed", "kuromatsu")
        run("systemctl", "start", "kuromatsu")
        print("ROLLED_BACK", backup, flush=True)
        raise


if __name__ == "__main__":
    main()

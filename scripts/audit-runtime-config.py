#!/usr/bin/env python3
"""Print configuration provenance and model/path settings without credentials."""
import json
import os
import pathlib
import subprocess

pid = subprocess.check_output(
    ["systemctl", "show", "kuromatsu", "-p", "MainPID", "--value"], text=True
).strip()
allowed = {"KUROMATSU_HOME", "KUROMATSU_CONFIG", "KUROMATSU_MODELS_DIR",
           "PICOCLAW_HOME", "PICOCLAW_CONFIG"}
if pid != "0":
    for item in pathlib.Path(f"/proc/{pid}/environ").read_bytes().split(b"\0"):
        key, _, value = item.partition(b"=")
        if key.decode() in allowed:
            print("ENV", key.decode(), value.decode())
    print("CWD", os.readlink(f"/proc/{pid}/cwd"))
    maps = pathlib.Path(f"/proc/{pid}/maps").read_text()
    print("MAPPED_MODELS", sorted({line.split()[-1] for line in maps.splitlines() if ".gguf" in line}))

def paths(value, prefix=""):
    if isinstance(value, dict):
        for key, child in value.items():
            name = f"{prefix}.{key}" if prefix else key
            if isinstance(child, str) and key in {
                "workspace", "model_path", "state_dir", "working_dir", "path", "dir", "home"
            }:
                print("PATH", name, child)
            elif isinstance(child, (dict, list)):
                paths(child, name)
    elif isinstance(value, list):
        for i, child in enumerate(value):
            paths(child, f"{prefix}[{i}]")

for path in [pathlib.Path("/var/lib/kuromatsu/config.json"),
             pathlib.Path("/var/lib/kuromatsu/workspace/config.json")]:
    if not path.exists():
        continue
    cfg = json.loads(path.read_text())
    print("CONFIG", str(path), "mtime", path.stat().st_mtime)
    defaults = cfg.get("agents", {}).get("defaults", {})
    print("DEFAULTS", {k: defaults.get(k) for k in ["model", "model_name", "provider", "workspace"]})
    print("GATEWAY", {k: cfg.get("gateway", {}).get(k) for k in ["host", "port"]})
    for model in cfg.get("model_list", []):
        print("MODEL", {k: model.get(k) for k in ["model_name", "model", "provider", "enabled"]})
    paths(cfg)

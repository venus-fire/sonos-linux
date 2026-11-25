#!/usr/bin/env python3
"""Keep chosen applications on chosen sinks.

Strawberry opens a new stream for every track, and a stream with no recorded
target follows the default sink. So moving the player to the Sonos by hand
works for one track and then silently reverts. This watcher moves it back.

Configuration is a JSON file at ~/.config/sonos-pin-app.json:

    {"target": "sonos_den", "apps": ["Strawberry"]}

Run as a systemd user service, see sonos-pin-app.service.
"""

import json
import os
import subprocess
import sys
import time

CONFIG = os.path.expanduser("~/.config/sonos-pin-app.json")


def log(msg):
    print(time.strftime("%H:%M:%S"), msg, flush=True)


def load_config():
    try:
        with open(CONFIG) as fh:
            cfg = json.load(fh)
    except (OSError, ValueError) as exc:
        log(f"cannot read {CONFIG}: {exc}")
        return None
    cfg.setdefault("apps", [])
    cfg.setdefault("target", "")
    return cfg


def pactl(*args):
    try:
        return subprocess.check_output(["pactl", *args], text=True,
                                       stderr=subprocess.DEVNULL)
    except Exception:
        return ""


def sink_ids():
    """name -> id, for every sink present."""
    out = {}
    for line in pactl("list", "short", "sinks").splitlines():
        fields = line.split("\t")
        if len(fields) > 1:
            out[fields[1]] = fields[0]
    return out


def streams():
    """[(input id, sink id, application.name)] for every playback stream."""
    out = []
    current = None
    for line in pactl("list", "sink-inputs").splitlines():
        line = line.strip()
        if line.startswith("Sink Input #"):
            current = {"id": line.split("#", 1)[1]}
        elif current is not None and line.startswith("Sink:"):
            current["sink"] = line.split(":", 1)[1].strip()
        elif current is not None and line.startswith("application.name = "):
            current["app"] = line.split("=", 1)[1].strip().strip('"')
            out.append(current)
            current = None
    return [(s.get("id"), s.get("sink"), s.get("app")) for s in out]


def enforce(cfg):
    target_name = cfg["target"]
    sinks = sink_ids()
    target = sinks.get(target_name)
    if target is None:
        return
    apps = {a.lower() for a in cfg["apps"]}
    for input_id, sink_id, app in streams():
        if not app or app.lower() not in apps:
            continue
        if sink_id == target:
            continue
        log(f"moving {app} stream {input_id} to {target_name}")
        pactl("move-sink-input", input_id, target)


def main():
    cfg = load_config()
    if not cfg:
        sys.exit(1)
    if not cfg["apps"]:
        log("no apps configured, nothing to do")
    log(f"pinning {cfg['apps']} to {cfg['target']}")
    proc = subprocess.Popen(["pactl", "subscribe"], stdout=subprocess.PIPE, text=True)
    last = 0.0
    while True:
        # A stream can appear before its sink or its application.name property
        # is readable, so the event is only a hint: re-read the whole list.
        line = proc.stdout.readline()
        if line:
            enforce(cfg)
        if time.time() - last > 5:
            enforce(cfg)
            last = time.time()


if __name__ == "__main__":
    main()

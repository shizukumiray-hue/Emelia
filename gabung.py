#!/usr/bin/env python3
"""gabung.py — gabung file proxy ke Data/Gabungan_sementara.txt (dedupe, sort, tanpa API check).

Usage:
  python3 gabung.py FILE1 [FILE2 ...]        # gabung ke Data/Gabungan_sementara.txt
  python3 gabung.py FILE -o TARGET.txt       # target custom

Format diterima:
  IP:PORT#CC            (legacy, tanpa org)
  IP,PORT,CC,ORG        (standar)
  IP,PORT               (tanpa negara)
"""
import ipaddress
import re
import sys
from collections import OrderedDict

DEFAULT_TARGET = "Data/Gabungan_sementara.txt"


def parse_line(line: str):
    """Return (ip, port, cc, org) or None."""
    line = line.strip()
    if not line or line.startswith("#"):
        return None
    # legacy: IP:PORT#CC
    m = re.match(r"^([\w.\-]+):(\d+)#([A-Za-z]{2})$", line)
    if m:
        return m.group(1), m.group(2), m.group(3).upper(), ""
    parts = line.split(",")
    if len(parts) < 2:
        return None
    ip = parts[0].strip()
    port = parts[1].strip()
    cc = parts[2].strip().upper()[:2] if len(parts) >= 3 else ""
    org = parts[3].strip() if len(parts) >= 4 else ""
    return ip, port, cc, org


def valid_ip(ip: str) -> bool:
    try:
        ipaddress.ip_address(ip)
        return True
    except ValueError:
        return False


def merge(sources, target):
    merged = OrderedDict()  # key: ip:port -> (ip, port, cc, org)

    def add(entry):
        ip, port, cc, org = entry
        if not valid_ip(ip) or not port.isdigit():
            return False
        key = f"{ip}:{port}"
        old = merged.get(key)
        if old is None:
            merged[key] = (ip, port, cc, org)
            return True
        if old[3] == "" and org != "":
            merged[key] = (ip, port, cc or old[2], org)
            return True
        return False

    stats = {"added": 0, "dup": 0, "skipped": 0, "files": 0, "from_target": 0}
    # target lama dulu — gabung, bukan timpa
    try:
        with open(target, encoding="utf-8", errors="replace") as f:
            for line in f:
                entry = parse_line(line)
                if entry is not None and add(entry):
                    stats["from_target"] += 1
    except FileNotFoundError:
        pass
    for path in sources:
        stats["files"] += 1
        try:
            with open(path, encoding="utf-8", errors="replace") as f:
                for line in f:
                    entry = parse_line(line)
                    if entry is None or not add(entry):
                        stats["skipped"] += 1
                    else:
                        stats["added"] += 1
        except OSError as e:
            print(f"❌ Gagal baca {path}: {e}")
            sys.exit(1)

    stats["dup"] = stats["skipped"]
    lines = sorted(merged.values(), key=lambda e: (e[2], e[0]))
    with open(target, "w", encoding="utf-8") as f:
        for ip, port, cc, org in lines:
            f.write(f"{ip},{port},{cc},{org}\n")
    stats["target"] = target
    stats["total"] = len(lines)
    return stats


if __name__ == "__main__":
    args = [a for a in sys.argv[1:] if a != "--"]
    target = DEFAULT_TARGET
    if "-o" in args:
        i = args.index("-o")
        target = args[i + 1]
        del args[i:i + 2]
    if not args:
        print(__doc__)
        sys.exit(1)
    s = merge(args, target)
    print(f"📁 {s['files']} file → {s['target']}")
    print(f"➕ {s['added']} baris valid | 🚫 {s['dup']} duplikat/invalid | 📊 total {s['total']}")

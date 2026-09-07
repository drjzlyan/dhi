#!/usr/bin/env python3
"""pin-gh-manifest.py — pin DHI's hermetic gh CLI (ADR-0011) into
internal/toolchain/registry/manifest.json by digesting the official
cli/cli release tarballs.

Unlike hermetic-git there is nothing DHI builds: cli/cli publishes
official binaries, so the trust chain matches rg/uv/node — the pin PR
carries the upstream URL + sha256 and is human-reviewed. This script
is the pin ceremony; it never writes the manifest without recomputing
every digest locally.

Usage:
  pin-gh-manifest.py --version 2.65.0 [--registry PATH]
"""
import argparse
import hashlib
import json
import os
import sys
import tempfile
import urllib.request

# "darwin/arm64" → (archive filename pattern, format)
PLATFORMS = {
    "darwin/arm64": ("gh_{v}_macOS_arm64.zip", "zip"),
    "linux/amd64": ("gh_{v}_linux_amd64.tar.gz", "tar.gz"),
}


def sha256_file(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--version", required=True)
    default_registry = os.path.join("internal", "toolchain", "registry", "manifest.json")
    ap.add_argument("--registry", default=default_registry)
    args = ap.parse_args()

    v = args.version.lstrip("v")
    platforms = {}
    for plat, (pattern, fmt) in PLATFORMS.items():
        fname = pattern.format(v=v)
        url = f"https://github.com/cli/cli/releases/download/v{v}/{fname}"
        with tempfile.TemporaryDirectory() as td:
            local = os.path.join(td, fname)
            print(f"downloading {url}")
            try:
                urllib.request.urlretrieve(url, local)
            except Exception as e:  # noqa: BLE001 — die visibly
                sys.exit(f"download failed for {plat}: {e}")
            digest = sha256_file(local)
        platforms[plat] = {
            "url": url,
            "sha256": digest,
            "format": fmt,
            "strip": 1,
            "bin_dir": "bin",
        }

    with open(args.registry) as f:
        reg = json.load(f)

    gh_tool = reg["tools"].get("gh", {"shims": ["gh"]})
    prev = gh_tool.get("platforms", {})
    gh_tool["version"] = v
    merged = dict(prev)
    merged.update(platforms)
    gh_tool["platforms"] = {k: merged[k] for k in sorted(merged)}
    missing = [p for p in PLATFORMS if p not in gh_tool["platforms"]]
    if missing:
        sys.exit(f"incomplete pin; missing platforms: {', '.join(missing)}")
    gh_tool.setdefault("shims", ["gh"])
    reg["tools"]["gh"] = gh_tool

    with open(args.registry, "w") as f:
        json.dump(reg, f, indent=2)
        f.write("\n")

    print(f"pinned hermetic gh v{v}:")
    for plat, spec in gh_tool["platforms"].items():
        print(f'  {plat}\n    {spec["url"]}\n    {spec["sha256"]}')


if __name__ == "__main__":
    main()

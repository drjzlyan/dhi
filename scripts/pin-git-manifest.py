#!/usr/bin/env python3
"""pin-git-manifest.py — generate the registry pin for a published
hermetic-git release and update internal/toolchain/registry/manifest.json.

Run by CI's pin-pr job after the release exists. Idempotent: reruns with
newer digests update in place, leaving every other tool untouched.

Checks before touching anything:
  - every dist/dhi-git-*.tar.gz hashes to its .sha256 sidecar
  - every expected platform slug is present

Usage:
  pin-git-manifest.py --dist DIR --version V [--registry PATH]
Env: GITHUB_REPOSITORY (set by Actions) supplies the download base URL.
"""
import argparse
import hashlib
import json
import os
import re
import sys

SLUG_TO_PLATFORM = {
    "darwin-arm64": "darwin/arm64",
    "linux-amd64": "linux/amd64",
}


def sha256_file(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--dist", required=True)
    ap.add_argument("--version", required=True)
    default_registry = os.path.join("internal", "toolchain", "registry", "manifest.json")
    ap.add_argument("--registry", default=default_registry)
    args = ap.parse_args()

    repo = os.environ.get("GITHUB_REPOSITORY")
    if not repo:
        sys.exit("GITHUB_REPOSITORY not set")

    dists = sorted(f for f in os.listdir(args.dist)
                   if f.startswith("dhi-git-") and f.endswith(".tar.gz"))
    if not dists:
        sys.exit(f"no dhi-git-*.tar.gz under {args.dist}")

    platforms = {}
    for fname in dists:
        path = os.path.join(args.dist, fname)
        actual = sha256_file(path)

        m = re.fullmatch(r"dhi-git-([0-9.]+)-(.+)\.tar\.gz", fname)
        if not m or m.group(1) != args.version:
            sys.exit(f"{fname}: does not match version {args.version}")
        slug = m.group(2)
        plat = SLUG_TO_PLATFORM.get(slug)
        if plat is None:
            sys.exit(f"{fname}: unknown platform slug {slug!r}")

        # build script writes "<name>.sha256", not "<name>.tar.gz.sha256"
        if not fname.endswith(".tar.gz"):
            sys.exit(f"{fname}: unexpected extension")
        sidecar = path[: -len(".tar.gz")] + ".sha256"
        if os.path.exists(sidecar):
            expected = open(sidecar).read().split()[0].strip().lower()
            if expected != actual:
                sys.exit(f"{fname}: digest mismatch vs sidecar "
                         f"({actual} != {expected})")
        else:
            sys.exit(f"{fname}: missing .sha256 sidecar")

        platforms[plat] = {
            "url": f"https://github.com/{repo}/releases/download/"
                   f"hermetic-git-v{args.version}/{fname}",
            "sha256": actual,
            "format": "tar.gz",
        }

    with open(args.registry) as f:
        reg = json.load(f)

    git_tool = reg["tools"].get("git", {"shims": ["git"]})
    prev = git_tool.get("platforms", {})
    git_tool["version"] = args.version
    merged = dict(prev)
    merged.update(platforms)
    git_tool["platforms"] = {k: merged[k] for k in sorted(merged)}
    # A pin is only complete once both target platforms are covered.
    missing = [p for p in SLUG_TO_PLATFORM.values() if p not in git_tool["platforms"]]
    if missing:
        sys.exit(f"incomplete pin; missing platforms: {', '.join(missing)}")
    reg["tools"]["git"] = git_tool

    with open(args.registry, "w") as f:
        json.dump(reg, f, indent=2)
        f.write("\n")

    print(f'pinned hermetic git v{args.version}:')
    for plat, spec in git_tool["platforms"].items():
        print(f'  {plat}\n    {spec["url"]}\n    {spec["sha256"]}')


if __name__ == "__main__":
    main()

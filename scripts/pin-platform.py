#!/usr/bin/env python3
"""pin-platform.py — pin the upstream-built tools (go, rg, uv, node, gh) for
more platforms into internal/toolchain/registry/manifest.json.

Trust chain: official vendor URL -> download -> sha256 computed here AND
compared with the checksum the vendor publishes (go.dev JSON, SHASUMS256.txt,
.sha256 sidecars, checksums.txt). A mismatch or a missing vendor checksum aborts;
the manifest is only written after every artifact verified. Extraction settings
(format/strip/bin_dir) are copied from the same tool's existing entry for the
same OS. The hermetic git is NOT handled here: DHI builds it in CI
(.github/workflows/release-git.yml, scripts/pin-git-manifest.py).

Usage: pin-platform.py --platform darwin/amd64 [--platform linux/arm64] [--registry PATH]
"""
import argparse, hashlib, json, os, sys, tempfile, urllib.request

ASSET = {  # tool -> platform -> (url template, checksum source)
    "go": {
        "darwin/amd64": "go{v}.darwin-amd64.tar.gz",
        "linux/arm64": "go{v}.linux-arm64.tar.gz",
    },
    "rg": {
        "darwin/amd64": "ripgrep-{v}-x86_64-apple-darwin.tar.gz",
        "linux/arm64": "ripgrep-{v}-aarch64-unknown-linux-gnu.tar.gz",
    },
    "uv": {
        "darwin/amd64": "uv-x86_64-apple-darwin.tar.gz",
        "linux/arm64": "uv-aarch64-unknown-linux-gnu.tar.gz",
    },
    "node": {
        "darwin/amd64": "node-v{v}-darwin-x64.tar.gz",
        "linux/arm64": "node-v{v}-linux-arm64.tar.gz",
    },
    "gh": {
        "darwin/amd64": "gh_{v}_macOS_amd64.zip",
        "linux/arm64": "gh_{v}_linux_arm64.tar.gz",
    },
}
BASE = {
    "go": "https://go.dev/dl/{name}",
    "rg": "https://github.com/BurntSushi/ripgrep/releases/download/{v}/{name}",
    "uv": "https://github.com/astral-sh/uv/releases/download/{v}/{name}",
    "node": "https://nodejs.org/dist/v{v}/{name}",
    "gh": "https://github.com/cli/cli/releases/download/v{v}/{name}",
}


def fetch(url):
    with urllib.request.urlopen(url, timeout=120) as r:
        return r.read()


def vendor_sha(tool, v, name, url):
    """The checksum the vendor itself publishes for this artifact."""
    if tool == "go":
        for rel in json.loads(fetch("https://go.dev/dl/?mode=json&include=all")):
            for f in rel["files"]:
                if f["filename"] == name:
                    return f["sha256"]
    elif tool == "node":
        for line in fetch(f"https://nodejs.org/dist/v{v}/SHASUMS256.txt").decode().splitlines():
            h, _, n = line.partition("  ")
            if n.strip() == name:
                return h
    elif tool in ("rg", "uv"):
        return fetch(url + ".sha256").decode().split()[0]
    elif tool == "gh":
        for line in fetch(f"https://github.com/cli/cli/releases/download/v{v}/gh_{v}_checksums.txt").decode().splitlines():
            h, _, n = line.partition("  ")
            if n.strip() == name:
                return h
    return None


def download(url):
    h, size = hashlib.sha256(), 0
    with tempfile.TemporaryFile() as tmp:
        with urllib.request.urlopen(url, timeout=600) as r:
            for chunk in iter(lambda: r.read(1 << 20), b""):
                h.update(chunk)
                size += len(chunk)
    return h.hexdigest(), size


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--platform", action="append", required=True)
    ap.add_argument("--registry", default=os.path.join("internal", "toolchain", "registry", "manifest.json"))
    args = ap.parse_args()
    m = json.load(open(args.registry))
    staged = []
    for plat in args.platform:
        osname = plat.split("/")[0]
        for tool, names in ASSET.items():
            if plat not in names:
                sys.exit(f"{tool}: no asset recipe for {plat}")
            t = m["tools"][tool]
            v = t["version"]
            name = names[plat].format(v=v)
            url = BASE[tool].format(v=v, name=name)
            sibling = next((p for k, p in t["platforms"].items() if k.startswith(osname + "/") and k != plat), None) \
                or next(iter(t["platforms"].values()))
            print(f"{tool} {plat}: {url}", flush=True)
            want = vendor_sha(tool, v, name, url)
            if not want:
                sys.exit(f"{tool} {plat}: the vendor publishes no checksum for {name}; refusing to pin")
            got, size = download(url)
            if got != want:
                sys.exit(f"{tool} {plat}: sha256 {got} != vendor's {want}; refusing")
            entry = {k: sibling[k] for k in ("format", "strip", "bin_dir") if k in sibling}
            entry = {"url": url, "sha256": got, **entry, "size": size}
            staged.append((tool, plat, entry))
    for tool, plat, entry in staged:
        m["tools"][tool]["platforms"][plat] = entry
    with open(args.registry, "w") as f:
        json.dump(m, f, indent=2)
        f.write("\n")
    print(f"pinned {len(staged)} artifacts; hermetic git still needs CI for these platforms")


if __name__ == "__main__":
    main()

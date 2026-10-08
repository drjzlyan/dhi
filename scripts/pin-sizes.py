#!/usr/bin/env python3
"""Record each pinned artifact's download size in the toolchain manifest.

The first-run screen shows what DHI is about to download (per tool and in
total) before asking. Sizes come from a HEAD request to the pinned URL
(redirects followed); re-run after changing any pin. Idempotent.

    scripts/pin-sizes.py [--check]

--check exits 1 when any size is missing or stale, without writing.
"""
import json
import sys
import urllib.request

PATH = "internal/toolchain/registry/manifest.json"


def head_size(url):
    req = urllib.request.Request(url, method="HEAD", headers={"User-Agent": "dhi-pin-sizes"})
    with urllib.request.urlopen(req, timeout=30) as r:
        n = r.headers.get("Content-Length")
        if not n:
            raise SystemExit(f"no Content-Length from {url}")
        return int(n)


def main():
    check = "--check" in sys.argv
    with open(PATH) as f:
        mf = json.load(f)
    stale = []
    for name, tool in mf["tools"].items():
        for plat, spec in tool["platforms"].items():
            want = head_size(spec["url"])
            if spec.get("size") != want:
                stale.append(f"{name} {plat}: {spec.get('size')} -> {want}")
                spec["size"] = want
    if check:
        if stale:
            print("stale sizes:\n  " + "\n  ".join(stale))
            return 1
        print("sizes up to date")
        return 0
    with open(PATH, "w") as f:
        json.dump(mf, f, indent=2)
        f.write("\n")
    print(f"updated {len(stale)} size(s)")
    for s in stale:
        print("  " + s)
    return 0


if __name__ == "__main__":
    sys.exit(main())

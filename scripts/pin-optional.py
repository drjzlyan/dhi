#!/usr/bin/env python3
"""pin-optional.py — pin DHI's optional managed toolchains (F-076): rust, jdk,
maven, gradle, dotnet, for every release platform, into
internal/toolchain/registry/manifest.json.

Trust chain: official vendor metadata -> the vendor's published checksum
(sha256, or sha512 where that is all the vendor publishes) is recorded as
the pin; the installer re-hashes every downloaded byte against it before
extracting. A missing vendor checksum aborts. Sizes come from HEAD
requests. Nothing is downloaded in full here.

Usage: pin-optional.py [--registry PATH] [tool ...]
"""
import argparse, json, re, sys, tomllib, urllib.request

PLATFORMS = ["darwin/arm64", "darwin/amd64", "linux/amd64", "linux/arm64"]
UA = {"User-Agent": "dhi-pin-optional"}


def get(url):
    with urllib.request.urlopen(urllib.request.Request(url, headers=UA), timeout=60) as r:
        return r.read()


def size(url):
    req = urllib.request.Request(url, method="HEAD", headers=UA)
    with urllib.request.urlopen(req, timeout=60) as r:
        n = int(r.headers.get("Content-Length") or 0)
    if n <= 0:
        sys.exit(f"no size for {url}")
    return n


def need(v, what):
    if not v:
        sys.exit(f"missing vendor checksum for {what}: refusing to pin")
    return v.strip().split()[0].lower()


def rust():
    ch = tomllib.loads(get("https://static.rust-lang.org/dist/channel-rust-stable.toml").decode())
    pkg = ch["pkg"]["rust"]
    version = pkg["version"].split()[0]
    triples = {"darwin/arm64": "aarch64-apple-darwin", "darwin/amd64": "x86_64-apple-darwin",
               "linux/amd64": "x86_64-unknown-linux-gnu", "linux/arm64": "aarch64-unknown-linux-gnu"}
    plats = {}
    for p, tr in triples.items():
        t = pkg["target"][tr]
        if not t.get("available"):
            sys.exit(f"rust {version} not available for {tr}")
        plats[p] = {"url": t["url"], "sha256": need(t["hash"], f"rust/{p}"), "format": "tar.gz",
                    "strip": 1, "bin_dir": "bin", "merge": "rust-components", "size": size(t["url"])}
    return {"version": version, "optional": True, "platforms": plats, "shims": ["rustc", "cargo", "rustdoc", "rustfmt", "cargo-fmt", "cargo-clippy", "clippy-driver"]}


def jdk():
    plats, version = {}, None
    for p, (os_, arch) in {"darwin/arm64": ("mac", "aarch64"), "darwin/amd64": ("mac", "x64"),
                           "linux/amd64": ("linux", "x64"), "linux/arm64": ("linux", "aarch64")}.items():
        d = json.loads(get(f"https://api.adoptium.net/v3/assets/latest/21/hotspot?architecture={arch}&image_type=jdk&os={os_}&vendor=eclipse"))
        pk = d[0]["binary"]["package"]
        v = d[0]["version"]["openjdk_version"]
        version = version or v
        if v != version:
            sys.exit(f"jdk versions differ across platforms: {version} vs {v}")
        home = "{home}/Contents/Home" if os_ == "mac" else "{home}"
        plats[p] = {"url": pk["link"], "sha256": need(pk.get("checksum"), f"jdk/{p}"), "format": "tar.gz",
                    "strip": 1, "bin_dir": ("Contents/Home/bin" if os_ == "mac" else "bin"),
                    "env": {"JAVA_HOME": home}, "size": pk["size"]}
    return {"version": version, "optional": True, "platforms": plats, "shims": ["java", "javac", "jar"]}


def maven():
    meta = get("https://repo.maven.apache.org/maven2/org/apache/maven/apache-maven/maven-metadata.xml").decode()
    vs = [v for v in re.findall(r"<version>([^<]+)</version>", meta) if re.fullmatch(r"3\.9\.\d+", v)]
    v = sorted(vs, key=lambda s: [int(x) for x in s.split(".")])[-1]
    url = f"https://repo.maven.apache.org/maven2/org/apache/maven/apache-maven/{v}/apache-maven-{v}-bin.tar.gz"
    spec = {"url": url, "sha512": need(get(url + ".sha512").decode(), "maven"), "format": "tar.gz",
            "strip": 1, "bin_dir": "bin", "size": size(url)}
    return {"version": v, "optional": True, "platforms": {p: dict(spec) for p in PLATFORMS}, "shims": ["mvn"]}


def gradle():
    cur = json.loads(get("https://services.gradle.org/versions/current"))
    url = cur["downloadUrl"]
    spec = {"url": url, "sha256": need(get(cur["checksumUrl"]).decode(), "gradle"), "format": "zip",
            "strip": 1, "bin_dir": "bin", "size": size(url)}
    return {"version": cur["version"], "optional": True, "platforms": {p: dict(spec) for p in PLATFORMS}, "shims": ["gradle"]}


def dotnet():
    idx = json.loads(get("https://builds.dotnet.microsoft.com/dotnet/release-metadata/releases-index.json"))
    lts = [c for c in idx["releases-index"] if c.get("release-type") == "lts" and c.get("support-phase") == "active"]
    ch = sorted(lts, key=lambda c: [int(x) for x in c["channel-version"].split(".")])[-1]
    rel = json.loads(get(ch["releases.json"]))
    sdk_v = rel["latest-sdk"]
    files = None
    for r in rel["releases"]:
        for s in [r.get("sdk")] + r.get("sdks", []):
            if s and s.get("version") == sdk_v:
                files = s["files"]
    if not files:
        sys.exit(f"dotnet sdk {sdk_v}: no files")
    rids = {"darwin/arm64": "osx-arm64", "darwin/amd64": "osx-x64", "linux/amd64": "linux-x64", "linux/arm64": "linux-arm64"}
    plats = {}
    for p, rid in rids.items():
        f = [f for f in files if f.get("rid") == rid and f["name"].endswith(".tar.gz") and "sdk" in f["name"]]
        if not f:
            sys.exit(f"dotnet: no tar.gz for {rid}")
        url = f[0]["url"]
        plats[p] = {"url": url, "sha512": need(f[0].get("hash"), f"dotnet/{p}"), "format": "tar.gz",
                    "env": {"DOTNET_ROOT": "{home}"}, "size": size(url)}
    return {"version": sdk_v, "optional": True, "platforms": plats, "shims": ["dotnet"]}


TOOLS = {"rust": rust, "jdk": jdk, "maven": maven, "gradle": gradle, "dotnet": dotnet}


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--registry", default="internal/toolchain/registry/manifest.json")
    ap.add_argument("tools", nargs="*")
    a = ap.parse_args()
    with open(a.registry) as f:
        mf = json.load(f)
    for name in a.tools or list(TOOLS):
        entry = TOOLS[name]()
        mf["tools"][name] = entry
        print(f"pinned {name} {entry['version']}", file=sys.stderr)
    with open(a.registry, "w") as f:
        json.dump(mf, f, indent=2)
        f.write("\n")


if __name__ == "__main__":
    main()

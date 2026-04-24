#!/usr/bin/env python3
"""Fetch Google Fonts CSS + woff2 files and rewrite to local paths.

Run from the repo root. Downloads .woff2 files into
web/public/fonts/<family-slug>/, dedupes on URL (Google Fonts variable-font
@font-face blocks for different weights often point to the same file), and
writes the rewritten CSS to web/src/fonts.css. Vite copies web/public/*
into dist/ during build; web/embed.go's //go:embed all:dist picks the
files up, so every byte ships inside the Go binary.

The script also:
  * Downloads the upstream SIL OFL 1.1 license for each family into
    web/public/fonts/<family-slug>/OFL.txt so license attribution ships
    alongside the binaries (required by OFL §5).
  * Writes a SHA-256 manifest at web/public/fonts/manifest.json so we can
    verify committed woff2 files on re-run and catch accidental corruption
    or partial downloads.

When changing the requested font weights / italics / opsz axes, edit
GOOGLE_FONTS_URL below and re-run. Commit the new woff2 files, the
regenerated web/src/fonts.css, the updated manifest.json, and any newly
fetched OFL.txt files.
"""
from __future__ import annotations

import hashlib
import json
import re
import subprocess
import sys
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent
FONTS_DIR = REPO_ROOT / "web" / "public" / "fonts"
CSS_OUT = REPO_ROOT / "web" / "src" / "fonts.css"
MANIFEST_OUT = FONTS_DIR / "manifest.json"

GOOGLE_FONTS_URL = (
    "https://fonts.googleapis.com/css2"
    "?family=Fraunces:ital,opsz,wght@0,9..144,400;0,9..144,500;0,9..144,600;1,9..144,400"
    "&family=Geist:wght@300..700"
    "&family=JetBrains+Mono:wght@400;500;600"
    "&display=swap"
)

# Subsets we ship. "latin" covers ASCII/English; "latin-ext" covers the
# diacritics used across French/German/Italian/Spanish/Nordic/Polish/Czech
# and is cheap. We drop cyrillic, cyrillic-ext, greek and vietnamese —
# ContextMatrix is a developer-facing tool authored in English; glyphs
# outside these ranges fall back to system fonts, which is fine.
SUBSETS_KEEP = {"latin", "latin-ext"}

# Upstream SIL OFL 1.1 license sources per family. Keyed by the slugified
# family name (matching the subdirectory under web/public/fonts/). Update
# when adding a new family, and re-run the script to fetch its license.
# Branch names vary across upstreams — these URLs were probed directly;
# re-verify before bumping.
LICENSE_SOURCES = {
    "fraunces": "https://raw.githubusercontent.com/undercasetype/Fraunces/master/OFL.txt",
    "geist": "https://raw.githubusercontent.com/vercel/geist-font/main/OFL.txt",
    "jetbrains-mono": "https://raw.githubusercontent.com/JetBrains/JetBrainsMono/master/OFL.txt",
}

UA = "Mozilla/5.0 (X11; Linux x86_64; rv:125.0) Gecko/20100101 Firefox/125.0"

HEADER = f"""/*
 * Self-hosted @font-face declarations for Fraunces, Geist, and JetBrains Mono.
 * Generated from the Google Fonts CSS response so unicode-range subsetting
 * and font-display settings match what Google serves. woff2 files live under
 * web/public/fonts/<family>/ and are emitted to dist/fonts/... by Vite, then
 * embedded into the Go binary via web/embed.go's //go:embed all:dist.
 *
 * Source URL (regenerate via scripts/fontfetch.py if weights or families
 * change):
 *   {GOOGLE_FONTS_URL}
 */

"""


def slug(s: str) -> str:
    return re.sub(r"[^a-z0-9]+", "-", s.lower()).strip("-")


def run(cmd: list[str]) -> subprocess.CompletedProcess[str]:
    res = subprocess.run(cmd, capture_output=True, text=True)
    if res.returncode != 0:
        print(f"FAILED: {' '.join(cmd)}\n{res.stderr}", file=sys.stderr)
        sys.exit(1)
    return res


def sha256_of(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as f:
        for chunk in iter(lambda: f.read(1 << 16), b""):
            h.update(chunk)
    return h.hexdigest()


def load_manifest() -> dict[str, str]:
    if not MANIFEST_OUT.exists():
        return {}
    try:
        data = json.loads(MANIFEST_OUT.read_text())
    except json.JSONDecodeError:
        return {}
    # Stored shape: { "files": { "<relative-path>": "<sha256-hex>", ... } }
    files = data.get("files")
    return dict(files) if isinstance(files, dict) else {}


def write_manifest(files: dict[str, str]) -> None:
    payload = {
        "source": GOOGLE_FONTS_URL,
        "files": dict(sorted(files.items())),
    }
    MANIFEST_OUT.write_text(json.dumps(payload, indent=2) + "\n")


def download_license(family_slug: str) -> None:
    """Fetch the upstream OFL license into the per-family directory.

    Idempotent: skips if the file already exists (licenses rarely change).
    Exits the script if the family has no known upstream URL — callers must
    add an entry to LICENSE_SOURCES before shipping a new family.
    """
    url = LICENSE_SOURCES.get(family_slug)
    if url is None:
        print(
            f"FAILED: no LICENSE_SOURCES entry for family '{family_slug}'. "
            f"Add an upstream OFL URL to scripts/fontfetch.py and re-run.",
            file=sys.stderr,
        )
        sys.exit(1)

    license_path = FONTS_DIR / family_slug / "OFL.txt"
    if license_path.exists() and license_path.stat().st_size > 0:
        return
    license_path.parent.mkdir(parents=True, exist_ok=True)
    print(f"↓ {family_slug}/OFL.txt  ({url})")
    run(["curl", "-sSfL", "-A", UA, "-o", str(license_path), url])


def main() -> None:
    print(f"Fetching CSS from Google Fonts …")
    css_res = run(["curl", "-sSfL", "-A", UA, GOOGLE_FONTS_URL])
    css = css_res.stdout

    block_re = re.compile(
        r"/\*\s*(?P<subset>[a-z-]+)\s*\*/\s*(?P<block>@font-face\s*\{[^}]*\})",
        re.MULTILINE,
    )
    family_re = re.compile(r"font-family:\s*'([^']+)'")
    style_re = re.compile(r"font-style:\s*(\S+);")
    url_re = re.compile(r"url\(([^)]+\.woff2)\)(\s*format\([^)]+\))?")

    blocks = list(block_re.finditer(css))
    if not blocks:
        print("No @font-face blocks returned by Google Fonts", file=sys.stderr)
        sys.exit(1)
    print(f"Parsed {len(blocks)} @font-face blocks")

    url_to_local: dict[str, Path] = {}
    new_css_parts: list[str] = []
    families_seen: set[str] = set()

    for m in blocks:
        subset = m.group("subset")
        if subset not in SUBSETS_KEEP:
            continue
        block = m.group("block")
        family = family_re.search(block).group(1)
        style = style_re.search(block).group(1)
        url_match = url_re.search(block)
        url = url_match.group(1)

        family_slug = slug(family)
        families_seen.add(family_slug)

        if url not in url_to_local:
            url_hash = hashlib.sha1(url.encode()).hexdigest()[:8]
            filename = f"{family_slug}-{style}-{subset}-{url_hash}.woff2"
            url_to_local[url] = FONTS_DIR / family_slug / filename

        local_path = url_to_local[url]
        public_url = f"/fonts/{local_path.parent.name}/{local_path.name}"

        new_block = url_re.sub(
            f"url({public_url}) format('woff2')", block
        )
        new_css_parts.append(f"/* {subset} */\n{new_block}")

    CSS_OUT.parent.mkdir(parents=True, exist_ok=True)
    CSS_OUT.write_text(HEADER + "\n".join(new_css_parts) + "\n")
    print(f"Wrote rewritten CSS → {CSS_OUT.relative_to(REPO_ROOT)}")
    print(f"Unique woff2 files: {len(url_to_local)} (from {len(blocks)} blocks)")

    # Fetch upstream licenses for every family we emitted CSS for.
    for family_slug in sorted(families_seen):
        download_license(family_slug)

    # Download woff2 files, verifying existing ones against the manifest.
    prev_manifest = load_manifest()
    new_manifest: dict[str, str] = {}

    for url, path in url_to_local.items():
        path.parent.mkdir(parents=True, exist_ok=True)
        rel = str(path.relative_to(FONTS_DIR))

        if path.exists() and path.stat().st_size > 0:
            actual = sha256_of(path)
            expected = prev_manifest.get(rel)
            if expected is None or expected == actual:
                new_manifest[rel] = actual
                continue
            print(
                f"!  sha256 mismatch on {rel}\n"
                f"   expected {expected[:16]}…\n"
                f"   got      {actual[:16]}…\n"
                f"   re-downloading",
                file=sys.stderr,
            )
            path.unlink()

        print(f"↓ {path.name}")
        run(["curl", "-sSfL", "-A", UA, "-o", str(path), url])
        new_manifest[rel] = sha256_of(path)

    write_manifest(new_manifest)

    total_bytes = sum(p.stat().st_size for p in FONTS_DIR.rglob("*.woff2"))
    print(
        f"Downloaded {len(url_to_local)} files "
        f"({total_bytes/1024:.0f} KB) into {FONTS_DIR.relative_to(REPO_ROOT)}"
    )
    print(f"Wrote manifest → {MANIFEST_OUT.relative_to(REPO_ROOT)}")


if __name__ == "__main__":
    main()

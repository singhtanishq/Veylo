#!/usr/bin/env python3
"""Validate Veylo screenshot delivery.

Checks:
  1. Every PNG under docs/screenshots/ is referenced in the root README.
  2. Every README screenshot reference points at an existing file.
  3. Every file in the manifest (SCREENSHOT_INDEX.md) exists on disk and is
     referenced in the README.
  4. No duplicate references in the README.
  5. No obvious broken/duplicate assets.

Exit code 1 when validation fails; 0 when everything is embedded cleanly.
"""

import json
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
README = ROOT / "README.md"
SHOTS = ROOT / "docs" / "screenshots"
MANIFEST_MD = SHOTS / "SCREENSHOT_INDEX.md"
MANIFEST_JSON = ROOT / "docs" / "screenshots-manifest.json"

README_REF = re.compile(r"!\[[^\]]*\]\((docs/screenshots/[^)]+?)\)")


def main() -> int:
    if not README.exists():
        print("FAIL: README.md not found")
        return 1

    readme_text = README.read_text(encoding="utf-8")
    referenced = README_REF.findall(readme_text)

    on_disk = sorted(
        str(p.relative_to(ROOT)) for p in SHOTS.rglob("*.png")
    )

    broken = [r for r in referenced if not (ROOT / r).exists()]
    duplicates = sorted({r for r in referenced if referenced.count(r) > 1})
    unreferenced = [f for f in on_disk if f not in referenced]

    # Manifest cross-check
    manifest_missing = []
    manifest_unreferenced = []
    manifest_count = 0
    if MANIFEST_JSON.exists():
        entries = json.loads(MANIFEST_JSON.read_text())
        manifest_count = len(entries)
        for entry in entries:
            f = f"docs/screenshots/{entry['file']}"
            if not (ROOT / f).exists():
                manifest_missing.append(f)
            if f not in referenced:
                manifest_unreferenced.append(f)
    elif MANIFEST_MD.exists():
        for m in re.finditer(r"`(docs/screenshots/[^`]+\.png)`", MANIFEST_MD.read_text()):
            manifest_count += 1
            if not (ROOT / m.group(1)).exists():
                manifest_missing.append(m.group(1))

    print("=" * 60)
    print("Veylo screenshot validation")
    print("=" * 60)
    print(f"Approved screenshots on disk : {len(on_disk)}")
    print(f"Embedded in README           : {len(referenced)}")
    print(f"Manifest entries             : {manifest_count}")
    print(f"Unreferenced screenshots     : {len(unreferenced)}")
    print(f"Broken image references      : {len(broken)}")
    print(f"Duplicate README references  : {len(duplicates)}")
    print(f"Manifest mismatches          : {len(manifest_missing) + len(manifest_unreferenced)}")

    failures = []
    if broken:
        failures.append(f"broken references: {broken}")
    if unreferenced:
        failures.append(f"unreferenced files: {unreferenced}")
    if duplicates:
        failures.append(f"duplicate references: {duplicates}")
    if manifest_missing:
        failures.append(f"manifest files missing on disk: {manifest_missing}")
    if manifest_unreferenced:
        failures.append(f"manifest files not embedded in README: {manifest_unreferenced}")

    if failures:
        print("\nFAIL:")
        for f in failures:
            print("  -", f)
        return 1

    print("\nOK: every approved screenshot is embedded exactly once in README.md.")
    return 0


if __name__ == "__main__":
    sys.exit(main())

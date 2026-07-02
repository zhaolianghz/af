#!/usr/bin/env python3
"""
add-spdx-headers.py — prepend a SPDX-License-Identifier line to every
source file in the AF Selector repo.

Why a script and not "find | sed | commit":
- Idempotent: skips files that already have the header (run on a
  freshly-cloned repo, run on an updated repo, no diff churn).
- Comment-style aware: knows the right prefix for .go, .ts, .tsx, .sh,
  .yml, .html, .css, Dockerfile.
- Idempotency check is not just a substring search — it looks for the
  SPDX line within the first ~10 lines of the file, where every language
  puts its leading comment block.
- Handles `#!shebang` (shell) and `# syntax=...` (Dockerfile) — those
  directives MUST stay on the very first line. SPDX goes after them.
- Re-fixes broken placements left by previous buggy runs (e.g. SPDX
  on line 1 with shebang pushed to line 2, or duplicate shebangs).

Run:
    python3 scripts/add-spdx-headers.py          # actually writes
    python3 scripts/add-spdx-headers.py --check # dry-run, exit 1 if changes needed

The dual-license SPDX expression (Apache-2.0 OR AGPL-3.0-or-later) is
hard-coded; if the project ever switches license, edit SPDX_LINE below.
"""

from __future__ import annotations

import argparse
import sys
from pathlib import Path

# Match the SPDX expression used in LICENSE / LICENSE-AGPL-3.0. The
# comment style prefix is added per-file based on extension.
SPDX_LINE = "SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later"

# (extension, comment_prefix, max_leading_lines_to_scan_for_existing)
COMMENT_STYLES: dict[str, tuple[str, int]] = {
    ".go":      ("//",  10),
    ".ts":      ("//",  10),
    ".tsx":     ("//",  10),
    ".js":      ("//",  10),
    ".jsx":     ("//",  10),
    ".mjs":     ("//",  10),
    ".cjs":     ("//",  10),
    ".sh":      ("#",   10),
    ".bash":    ("#",   10),
    ".zsh":     ("#",   10),
    ".yml":     ("#",   10),
    ".yaml":    ("#",   10),
    ".html":    ("<!--", 10),
    ".xml":     ("<!--", 10),
    ".tmpl":    ("<!--", 10),
    ".css":     ("/*",  10),
    ".scss":    ("//",  10),
    ".less":    ("//",  10),
    ".sql":     ("--",  10),
    ".proto":   ("//",  10),
}

# Files named exactly "Dockerfile" / "Containerfile" or ending in
# ".dockerfile" / ".containerfile" use `#` (the syntax= directive
# must also stay on line 1).
DOCKERFILE_NAMES = {"Dockerfile", "Containerfile"}
DOCKERFILE_EXTS = {".dockerfile", ".containerfile"}

# Directories we never touch.
SKIP_DIRS = {
    ".git",
    "node_modules",
    "dist",
    "build",
    "bin",
    "coverage",
    "playwright-report",
    "test-results",
    "vendor",
    ".venv",
    "__pycache__",
}

# Files we never touch (root-level meta files; not source code).
SKIP_FILES = {
    "LICENSE",
    "LICENSE-AGPL-3.0",
    "CHANGELOG.md",
    "README.md",
    "TODOS.md",
    "DX_REVIEW.md",
    "FE1_PLAN.md",
    "FE2_PLAN.md",
    "package-lock.json",
    "go.sum",
    ".gitignore",
    ".dockerignore",
    ".env.example",
    "node_modules",
}

# Embedded binary artifacts that some editors create; never edit.
SKIP_BIN_EXTENSIONS = {
    ".png", ".jpg", ".jpeg", ".gif", ".pdf", ".ico",
    ".woff", ".woff2", ".ttf", ".eot",
}


def comment_for(path: Path) -> str:
    """Return the SPDX comment line for this file's comment style."""
    if path.name in DOCKERFILE_NAMES or path.suffix.lower() in DOCKERFILE_EXTS:
        return f"# {SPDX_LINE}\n"
    ext = path.suffix.lower()
    style = COMMENT_STYLES.get(ext)
    if not style:
        return ""
    prefix, _ = style
    if prefix == "<!--":
        return f"<!-- {SPDX_LINE} -->\n"
    if prefix == "/*":
        return f"/* {SPDX_LINE} */\n"
    return f"{prefix} {SPDX_LINE}\n"


def first_line_after_reserved_directive(path: Path) -> int:
    """If the file starts with `#!shebang` (shell) or `# syntax=...`
    (Dockerfile), return the byte offset of the END of that line so
    the SPDX header is inserted AFTER it. Otherwise return 0 (insert
    at the very start)."""
    try:
        first = path.open("r", encoding="utf-8", errors="replace").readline()
    except OSError:
        return 0
    stripped = first.strip()
    if stripped.startswith("#!") or stripped.startswith("# syntax="):
        return len(first.encode("utf-8"))
    return 0


def already_has_spdx(path: Path) -> bool:
    """True if the file already has an SPDX marker in the first ~10
    non-empty lines (covers 'first line' AND 'after a 1-5 line
    leading doc-comment' placements)."""
    is_dockerfile = path.name in DOCKERFILE_NAMES or path.suffix.lower() in DOCKERFILE_EXTS
    if not is_dockerfile:
        style = COMMENT_STYLES.get(path.suffix.lower())
        if not style:
            return False
        prefix, max_check = style
    else:
        prefix, max_check = ("#", 10)
    try:
        with path.open("r", encoding="utf-8", errors="replace") as f:
            for i, line in enumerate(f):
                if i >= max_check:
                    break
                if "SPDX-License-Identifier" in line:
                    return True
                stripped = line.strip()
                if not stripped:
                    continue
                if prefix == "<!--":
                    if not stripped.startswith("<!--"):
                        return False
                elif prefix == "/*":
                    if not (stripped.startswith("/*") or stripped.startswith("//")):
                        return False
                else:
                    if not stripped.startswith(prefix):
                        return False
        return False
    except (OSError, UnicodeDecodeError):
        return False


def has_broken_placement(path: Path) -> bool:
    """True if the file has a "broken" SPDX placement that needs
    re-fixing:
      (a) SPDX on line 1, shebang / syntax= on line 2
      (b) 2+ shebang or 2+ syntax= lines in the first 5 lines AND
          an SPDX line is among them (the script inserted SPDX
          and then duplicated the shebang on the next run)
    """
    try:
        with path.open("r", encoding="utf-8", errors="replace") as f:
            first5 = [f.readline().rstrip("\n") for _ in range(5)]
    except OSError:
        return False
    stripped = [ln.strip() for ln in first5 if ln]
    if not stripped:
        return False
    # Case (a)
    if stripped[0].startswith("# SPDX-License-Identifier") and len(stripped) >= 2:
        if stripped[1].startswith("#!") or stripped[1].startswith("# syntax="):
            return True
    # Case (b)
    shebang_count = sum(1 for ln in stripped if ln.startswith("#!"))
    syntax_count = sum(1 for ln in stripped if ln.startswith("# syntax="))
    spdx_present = any(ln.startswith("# SPDX-License-Identifier") for ln in stripped)
    if (shebang_count >= 2 or syntax_count >= 2) and spdx_present:
        return True
    return False


def should_skip(path: Path) -> bool:
    if path.name in SKIP_FILES:
        return True
    if path.suffix.lower() in SKIP_BIN_EXTENSIONS:
        return True
    for parent in path.parents:
        if parent.name in SKIP_DIRS:
            return True
    return False


def iter_targets(root: Path):
    for p in root.rglob("*"):
        if not p.is_file():
            continue
        if should_skip(p):
            continue
        if (
            p.suffix.lower() in COMMENT_STYLES
            or p.name in DOCKERFILE_NAMES
            or p.suffix.lower() in DOCKERFILE_EXTS
        ):
            yield p


def add_header(path: Path) -> bool:
    """Insert the SPDX header into `path`. Idempotent (no-op if the
    header is already correctly placed). Returns True if the file was
    modified."""
    if not comment_for(path):
        return False
    original = path.read_bytes()
    try:
        original.decode("utf-8")
    except UnicodeDecodeError:
        return False
    offset = first_line_after_reserved_directive(path)
    insert = comment_for(path).encode("utf-8")
    new_content = original[:offset] + insert + original[offset:]
    if new_content == original:
        return False
    path.write_bytes(new_content)
    return True


def fix_broken(path: Path) -> bool:
    """Reorder a file with a broken SPDX placement to the canonical
    form (shebang / syntax on line 1, SPDX on line 2, then content).
    Returns True if the file was modified."""
    try:
        text = path.read_bytes().decode("utf-8", errors="replace")
    except OSError:
        return False
    lines = text.splitlines(keepends=True)
    # Find the first shebang/syntax line and keep it as the first line.
    first_directive = None
    for ln in lines:
        s = ln.strip()
        if s.startswith("#!") or s.startswith("# syntax="):
            first_directive = s
            break
    cleaned: list[str] = []
    for ln in lines:
        s = ln.strip()
        # Drop any leading SPDX line.
        if s.startswith("# SPDX-License-Identifier"):
            continue
        # Drop any duplicate of the first directive.
        if first_directive is not None and s == first_directive:
            continue
        cleaned.append(ln)
    new_text = "".join(cleaned)
    if new_text == text:
        return False
    path.write_bytes(new_text.encode("utf-8"))
    # Now insert the SPDX header in the right place.
    return add_header(path)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("root", nargs="?", default=".", help="project root (default: cwd)")
    parser.add_argument("--check", action="store_true", help="dry-run; exit 1 if any change needed")
    args = parser.parse_args()

    root = Path(args.root).resolve()
    targets = sorted(iter_targets(root))

    changed: list[Path] = []
    skipped_no_op: list[Path] = []
    skipped_unknown: list[Path] = []

    for p in targets:
        if has_broken_placement(p):
            if not args.check:
                fix_broken(p)
            changed.append(p)
            continue
        if already_has_spdx(p):
            skipped_no_op.append(p)
            continue
        if not comment_for(p):
            skipped_unknown.append(p)
            continue
        if args.check:
            changed.append(p)
        else:
            if add_header(p):
                changed.append(p)
            else:
                skipped_no_op.append(p)

    print(f"Scanned {len(targets)} files under {root}")
    print(f"  changed: {len(changed)}")
    print(f"  already have SPDX: {len(skipped_no_op)}")
    print(f"  unknown style (skipped): {len(skipped_unknown)}")

    if args.check and changed:
        print("\nFiles that would get a header (first 20):")
        for p in changed[:20]:
            rel = p.relative_to(root)
            print(f"  {rel}")
        return 1
    if changed and not args.check:
        print("\nAdded/fixed SPDX header in (first 20):")
        for p in changed[:20]:
            rel = p.relative_to(root)
            print(f"  {rel}")
    return 0


if __name__ == "__main__":
    sys.exit(main())

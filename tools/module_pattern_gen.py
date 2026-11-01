#!/usr/bin/env python3
"""
Module path pattern generator for mem-sys.

Scans a codebase root directory, identifies potential module roots
(based on manifest files or source directories), and generates
path_patterns JSON suitable for mem-sys Module creation.

Usage:
    python3 tools/module_pattern_gen.py /path/to/codebase -o modules.json

Output example:
    {
      "root": "/path/to/codebase",
      "module_count": 3,
      "path_patterns": [
        "services/auth/**",
        "services/billing/**",
        "packages/core/**"
      ]
    }
"""

import argparse
import fnmatch
import json
import os
import sys
from pathlib import Path

# Manifest files that strongly indicate a module root
MANIFEST_FILES = {
    "go.mod", "package.json", "package-lock.json",
    "Cargo.toml", "Cargo.lock",
    "setup.py", "pyproject.toml", "requirements.txt",
    "pom.xml", "build.gradle", "build.gradle.kts",
    "CMakeLists.txt", "Makefile",
    ".csproj", ".sln",
    "composer.json",
    "mix.exs",
    "Cargo.toml",
}

# Source file extensions used as a weaker signal
SOURCE_EXTENSIONS = {
    ".go", ".rs", ".py", ".js", ".ts", ".jsx", ".tsx",
    ".java", ".kt", ".kts",
    ".cpp", ".c", ".h", ".hpp", ".cc",
    ".cs", ".rb", ".php", ".swift", ".m", ".mm",
    ".ex", ".exs",
}

DEFAULT_EXCLUDE_DIRS = {
    ".git", ".svn", ".hg",
    "node_modules", ".venv", "venv", "__pycache__",
    "target", "build", "dist", ".out", "out",
    ".idea", ".vscode", ".next", ".cache",
    ".gradle", ".m2",
}


def _is_excluded(dirname, exclude_patterns):
    if dirname in DEFAULT_EXCLUDE_DIRS:
        return True
    for pat in exclude_patterns:
        if fnmatch.fnmatch(dirname, pat):
            return True
    return False


def _has_manifest(filenames):
    return bool(MANIFEST_FILES.intersection(filenames))


def _has_source_files(directory):
    try:
        with os.scandir(directory) as it:
            for entry in it:
                if entry.is_file():
                    if Path(entry.name).suffix in SOURCE_EXTENSIONS:
                        return True
    except OSError:
        pass
    return False


def find_module_roots(root_path, exclude_patterns, manifest_only=False):
    """Walk root_path and return relative paths of directories that look
    like module roots (contain a manifest file and/or source files)."""
    root_path = Path(root_path).resolve()
    roots = []

    for dirpath, dirnames, filenames in os.walk(root_path):
        # Prune excluded directories in-place so we don't descend into them
        dirnames[:] = [
            d for d in dirnames
            if not _is_excluded(d, exclude_patterns)
        ]

        has_manifest = _has_manifest(filenames)
        if manifest_only and not has_manifest:
            continue

        if has_manifest or _has_source_files(dirpath):
            rel = os.path.relpath(dirpath, root_path)
            if rel == ".":
                rel = ""
            roots.append(rel)

    return sorted(set(roots))


def build_patterns(roots, prefix=""):
    """Turn relative root paths into glob path patterns."""
    patterns = []
    for root in roots:
        pattern = f"{prefix}{root}/**" if root else f"{prefix}**"
        patterns.append(pattern)
    return patterns


def main():
    parser = argparse.ArgumentParser(
        description="Generate mem-sys module path_patterns from a codebase."
    )
    parser.add_argument(
        "root",
        help="Root path of the codebase to scan",
    )
    parser.add_argument(
        "-o", "--output",
        help="Write JSON output to this file (default: stdout)",
    )
    parser.add_argument(
        "-e", "--exclude", action="append", default=[],
        help="Exclude directories matching this glob pattern (repeatable). "
             "Example: -e 'test*' -e '*_old'",
    )
    parser.add_argument(
        "--manifest-only", action="store_true",
        help="Only include directories that contain a manifest file "
             "(e.g. go.mod, package.json, Cargo.toml).",
    )
    parser.add_argument(
        "--prefix", default="",
        help="String to prepend to every generated path pattern.",
    )

    args = parser.parse_args()

    root_path = Path(args.root)
    if not root_path.is_dir():
        print(f"Error: not a directory: {args.root}", file=sys.stderr)
        sys.exit(1)

    roots = find_module_roots(
        root_path,
        exclude_patterns=args.exclude,
        manifest_only=args.manifest_only,
    )

    patterns = build_patterns(roots, prefix=args.prefix)

    result = {
        "root": str(root_path.resolve()),
        "module_count": len(patterns),
        "path_patterns": patterns,
    }

    output = json.dumps(result, indent=2)

    if args.output:
        with open(args.output, "w") as f:
            f.write(output + "\n")
        print(f"Wrote {len(patterns)} pattern(s) to {args.output}")
    else:
        print(output)


if __name__ == "__main__":
    main()

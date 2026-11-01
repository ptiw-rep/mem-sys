# Module Path Pattern Generator

`tools/module_pattern_gen.py` is a small Python utility that scans a codebase root directory, identifies potential module roots, and generates `path_patterns` JSON suitable for creating modules in mem-sys.

No dependencies beyond the Python 3 standard library.

## How it works

The script walks the directory tree starting at the given root. For each directory, it decides whether that directory looks like a **module root** based on two signals:

1. **Manifest files** — if the directory contains a well-known manifest file (e.g. `go.mod`, `package.json`, `Cargo.toml`, `pom.xml`, `setup.py`, etc.), it is treated as a module root.
2. **Source files** — if the directory contains at least one source file (`.go`, `.rs`, `.py`, `.js`, `.java`, etc.), it is also treated as a module root.

Directories matching common exclusion patterns (`.git`, `node_modules`, `venv`, `target`, `build`, `.idea`, etc.) are skipped.

Each identified module root is converted into a glob path pattern of the form `<relative-path>/**`, which maps directly to the `path_patterns` field of a mem-sys `Module`.

## Usage

```bash
python3 tools/module_pattern_gen.py <root-path> [options]
```

### Arguments

| Argument | Description |
|---|---|
| `<root-path>` | Root directory of the codebase to scan (required). |
| `-o, --output FILE` | Write JSON output to this file instead of stdout. |
| `-e, --exclude PATTERN` | Exclude directories matching this glob pattern. Repeatable. Example: `-e 'test*' -e '*_old'`. |
| `--manifest-only` | Only include directories that contain a manifest file (ignore source-file-only directories). |
| `--prefix STRING` | Prepend this string to every generated path pattern. |

### Exit codes

| Code | Meaning |
|---|---|
| `0` | Success |
| `1` | Error (e.g. root path is not a directory) |

## Output format

The script prints (or writes to `-o`) a JSON object:

```json
{
  "root": "/path/to/codebase",
  "module_count": 3,
  "path_patterns": [
    "services/auth/**",
    "services/billing/**",
    "packages/core/**"
  ]
}
```

| Field | Meaning |
|---|---|
| `root` | Absolute path of the scanned codebase root. |
| `module_count` | Number of module patterns generated. |
| `path_patterns` | Array of glob patterns, one per detected module root. |

## Examples

### 1. Basic scan (stdout)

```bash
python3 tools/module_pattern_gen.py /home/user/my-repo --exclude .git --exclude vendor
```

### 2. Save to a file

```bash
python3 tools/module_pattern_gen.py /home/user/my-repo \
  --exclude .git --exclude vendor --exclude "test*" \
  -o modules.json
```

### 3. Manifest-only (ignore directories that only have loose source files)

```bash
python3 tools/module_pattern_gen.py /home/user/my-repo \
  --manifest-only --exclude .git -o modules.json
```

### 4. With a path prefix

Useful when your code lives under a subdirectory:

```bash
python3 tools/module_pattern_gen.py /home/user/my-repo \
  --prefix "src/" --exclude .git -o modules.json
```

Produces patterns like `src/services/auth/**`.

## Using the output with mem-sys

The generated `path_patterns` array maps directly to the `path_patterns` field when creating a mem-sys module via the REST API:

```bash
curl -s -H "Authorization: Bearer admin-alice" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "auth",
    "description": "Authentication module",
    "path_patterns": ["services/auth/**"],
    "exclude_patterns": [],
    "owner_ids": ["admin-alice"]
  }' \
  http://localhost:8080/api/v1/modules
```

You can also generate patterns for multiple modules and post them individually, or hand-edit the JSON before submitting.

## Limitations

- The script heuristically detects module roots; it does not parse actual import graphs or language-specific module systems.
- A directory containing only non-source files (e.g. only `README.md`) will not be detected unless it also contains a manifest file.
- Nested module roots: if directory `A` and subdirectory `A/B` both look like module roots, both will be emitted as separate patterns (`A/**` and `A/B/**`). Deduplicate manually if needed.

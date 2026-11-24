"""Check registry.json against the CPA plugin store rules this plugin relies on
(CPA internal/pluginstore: schema_version 1, github-release install).

usage: python3 scripts/check_registry.py [registry.json]
"""
import json
import re
import sys

PLUGIN_ID = "newapi-meter"  # must equal the library file name stem
REPOSITORY = "https://github.com/qiyi71w/cpa-plugin-newapi-meter"
# Same patterns as the store's pluginIDPattern / pluginVersionPattern.
ID_PATTERN = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$")
VERSION_PATTERN = re.compile(r"^[0-9][0-9A-Za-z.+-]*$")
REPOSITORY_PATTERN = re.compile(r"^https://github\.com/[^/?#\s]+/[^/?#\s]+$")


def fail(message):
    sys.exit(f"FAIL {message}")


def main(path):
    with open(path, encoding="utf-8") as f:
        registry = json.load(f)
    if registry.get("schema_version") != 1:
        fail(f"schema_version is {registry.get('schema_version')!r}, want 1")
    plugins = registry.get("plugins")
    if not isinstance(plugins, list) or not plugins:
        fail("plugins must be a non-empty list")
    ids = [p.get("id") for p in plugins]
    if len(set(ids)) != len(ids):
        fail(f"duplicate plugin ids {ids}")
    print("PASS schema_version 1")

    matches = [p for p in plugins if p.get("id") == PLUGIN_ID]
    if len(matches) != 1:
        fail(f"want exactly one plugin with id {PLUGIN_ID!r}, ids {ids}")
    plugin = matches[0]
    for field in ("id", "name", "description", "author", "repository"):
        value = plugin.get(field)
        if not isinstance(value, str) or not value.strip() or value != value.strip():
            fail(f"{field} must be a non-empty string without surrounding spaces, got {value!r}")
    print("PASS required fields: id, name, description, author, repository")

    if not ID_PATTERN.match(plugin["id"]):
        fail(f"id {plugin['id']!r} does not match {ID_PATTERN.pattern}")
    print(f"PASS id {plugin['id']!r} matches {ID_PATTERN.pattern}")

    repository = plugin["repository"]
    if repository != REPOSITORY or not REPOSITORY_PATTERN.match(repository) or repository.endswith(".git"):
        fail(f"repository {repository!r}, want exactly {REPOSITORY}")
    print(f"PASS repository {repository}")

    install = plugin.get("install") or {}
    if (install.get("type") or "github-release").lower() != "github-release":
        fail(f"install type {install.get('type')!r}: schema_version 1 sources install from GitHub Releases")
    version = plugin.get("version")
    if version is not None and (not isinstance(version, str) or version.startswith("v") or not VERSION_PATTERN.match(version)):
        fail(f"version {version!r} is not a valid store version")
    print("PASS install type github-release (the latest Release is the version source)")


if __name__ == "__main__":
    if len(sys.argv) > 2:
        sys.exit(__doc__)
    main(sys.argv[1] if len(sys.argv) == 2 else "registry.json")

"""Release packaging for the CPA plugin store (github-release install type).

usage:
  python3 scripts/package.py version <github-ref> <run-number>
      Print the package version: <semver> for a refs/tags/v<semver> ref,
      otherwise the placeholder 0.0.0-ci.<run-number>.
  python3 scripts/package.py pack <version> <dist-dir> <out-dir>
      Zip dist/<goos>/<goarch>/newapi-meter.<ext> for all six targets into
      <out-dir>/newapi-meter_<version>_<goos>_<goarch>.zip (the library alone,
      at the zip root) and write <out-dir>/checksums.txt (sha256).
  python3 scripts/package.py verify <version> <dir>
      Check a packaged directory: exactly the six zips plus checksums.txt,
      one library per zip at its root built for the zip's platform, and
      checksums.txt matching every zip.
"""
import hashlib
import os
import re
import stat
import struct
import sys
import zipfile

PLUGIN_ID = "newapi-meter"
TARGETS = [(goos, goarch) for goos in ("linux", "windows", "darwin") for goarch in ("amd64", "arm64")]
EXT = {"linux": "so", "windows": "dll", "darwin": "dylib"}
# The CPA store derives the version from the tag and requires this shape.
STORE_VERSION = re.compile(r"^[0-9][0-9A-Za-z.+-]*$")
SEMVER = re.compile(
    r"^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)"
    r"(?:-((?:0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*))*))?"
    r"(?:\+([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$"
)
CHECKSUM_LINE = re.compile(r"^([0-9a-f]{64})  (\S+)$")
# Fixed entry timestamp so the same library always yields the same zip.
ZIP_TIME = (1980, 1, 1, 0, 0, 0)


def library_name(goos):
    return f"{PLUGIN_ID}.{EXT[goos]}"


def archive_name(version, goos, goarch):
    return f"{PLUGIN_ID}_{version}_{goos}_{goarch}.zip"


def fail(message):
    sys.exit(f"FAIL {message}")


def check_version(version):
    if not STORE_VERSION.match(version):
        fail(f"version {version!r} is not a valid store version")


def package_version(ref, run_number):
    if ref.startswith("refs/tags/"):
        tag = ref[len("refs/tags/"):]
        if not tag.startswith("v") or not SEMVER.match(tag[1:]):
            fail(f"tag {tag!r} is not v<semver>")
        return tag[1:]
    if not run_number.isdigit():
        fail(f"run number {run_number!r} is not a number")
    return f"0.0.0-ci.{run_number}"


def sha256(path):
    digest = hashlib.sha256()
    with open(path, "rb") as f:
        for block in iter(lambda: f.read(1 << 20), b""):
            digest.update(block)
    return digest.hexdigest()


def binary_platform(data):
    """Return (goos, goarch) of an ELF, PE or Mach-O image, or None."""
    if data[:4] == b"\x7fELF" and len(data) >= 20:
        endian = "<" if data[5] == 1 else ">"
        machine = struct.unpack_from(endian + "H", data, 18)[0]
        return "linux", {62: "amd64", 183: "arm64"}.get(machine)
    if data[:2] == b"MZ" and len(data) >= 0x40:
        pe = struct.unpack_from("<I", data, 0x3C)[0]
        if data[pe:pe + 4] == b"PE\0\0":
            machine = struct.unpack_from("<H", data, pe + 4)[0]
            return "windows", {0x8664: "amd64", 0xAA64: "arm64"}.get(machine)
    if data[:4] == b"\xcf\xfa\xed\xfe" and len(data) >= 8:
        cputype = struct.unpack_from("<I", data, 4)[0]
        return "darwin", {0x01000007: "amd64", 0x0100000C: "arm64"}.get(cputype)
    return None


def pack(version, dist, out):
    check_version(version)
    os.makedirs(out, exist_ok=True)
    lines = []
    for goos, goarch in TARGETS:
        lib = os.path.join(dist, goos, goarch, library_name(goos))
        if not os.path.isfile(lib):
            fail(f"missing {lib}")
        with open(lib, "rb") as f:
            data = f.read()
        if binary_platform(data) != (goos, goarch):
            fail(f"{lib} is not a {goos}/{goarch} library")
        name = archive_name(version, goos, goarch)
        entry = zipfile.ZipInfo(library_name(goos), ZIP_TIME)
        entry.create_system = 3  # unix, so the mode below is honoured on every host
        entry.external_attr = (stat.S_IFREG | 0o755) << 16
        entry.compress_type = zipfile.ZIP_DEFLATED
        path = os.path.join(out, name)
        with zipfile.ZipFile(path, "w") as z:
            z.writestr(entry, data)
        lines.append(f"{sha256(path)}  {name}\n")
        print(f"packed {name}")
    with open(os.path.join(out, "checksums.txt"), "w", newline="\n") as f:
        f.writelines(lines)
    print("wrote checksums.txt")


def verify(version, directory):
    check_version(version)
    expected = {archive_name(version, goos, goarch): (goos, goarch) for goos, goarch in TARGETS}
    present = sorted(os.listdir(directory))
    if present != sorted([*expected, "checksums.txt"]):
        fail(f"files {present}, want the six zips and checksums.txt")
    print(f"PASS file names: {len(expected)} zips for version {version} + checksums.txt")

    for name, (goos, goarch) in expected.items():
        with zipfile.ZipFile(os.path.join(directory, name)) as z:
            entries = z.infolist()
            if [e.filename for e in entries] != [library_name(goos)]:
                fail(f"{name} contains {[e.filename for e in entries]}, want only {library_name(goos)} at the root")
            entry = entries[0]
            # Same rule as the store: a regular file, or no file type recorded.
            if entry.is_dir() or stat.S_IFMT(entry.external_attr >> 16) not in (0, stat.S_IFREG):
                fail(f"{name}: {entry.filename} is not a regular file")
            data = z.read(entry)  # also checks the CRC
        platform = binary_platform(data)
        if platform != (goos, goarch):
            fail(f"{name}: library is built for {platform}, want {(goos, goarch)}")
        print(f"PASS {name}: only {entry.filename} at the root, {goos}/{goarch} binary, {len(data)} bytes")

    with open(os.path.join(directory, "checksums.txt"), "rb") as f:
        text = f.read().decode("ascii")
    listed = {}
    for line in text.splitlines():
        match = CHECKSUM_LINE.match(line)
        if not match:
            fail(f"checksums.txt: malformed line {line!r}")
        digest, name = match.groups()
        if name in listed:
            fail(f"checksums.txt: {name} listed twice")
        listed[name] = digest
    if sorted(listed) != sorted(expected):
        fail(f"checksums.txt lists {sorted(listed)}, want {sorted(expected)}")
    for name, digest in listed.items():
        if sha256(os.path.join(directory, name)) != digest:
            fail(f"checksums.txt: sha256 mismatch for {name}")
    print(f"PASS checksums.txt: sha256 matches all {len(listed)} zips")


def main(argv):
    if len(argv) == 4 and argv[1] == "version":
        print(package_version(argv[2], argv[3]))
    elif len(argv) == 5 and argv[1] == "pack":
        pack(argv[2], argv[3], argv[4])
    elif len(argv) == 4 and argv[1] == "verify":
        verify(argv[2], argv[3])
    else:
        sys.exit(__doc__)


if __name__ == "__main__":
    main(sys.argv)

#!/usr/bin/env python3
"""Preserve matching installation configuration in a root-private local backup.

Base64 preserves file bytes; it is NOT encryption. Encrypt before copying off-host.
Run on a trusted Linux Docker host as root. Never overwrite an existing restore.
"""

import argparse
import base64
import binascii
from datetime import datetime
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import sys

MAX_FILE_BYTES = 1024 * 1024
MAX_TOTAL_BYTES = 8 * MAX_FILE_BYTES
MAX_BUNDLE_BYTES = 16 * MAX_FILE_BYTES
REQUIRED = {
    ".env", ".secrets/database_url", ".secrets/postgres_password",
    ".secrets/app_database_password", ".secrets/bootstrap_password",
    ".secrets/totp_key", ".secrets/smtp_password",
}
ARTIFACTS = ("database.dump", "files.tar.gz")


def directory(path, private=False):
    info = path.lstat()
    if not stat.S_ISDIR(info.st_mode):
        raise ValueError("Expected a real directory")
    if info.st_mode & (0o077 if private else 0o022):
        raise ValueError("Unsafe directory permissions")
    if private and info.st_uid != 0:
        raise ValueError("Private configuration must belong to root")


def regular(path):
    # O_NONBLOCK prevents a replaced FIFO from hanging the backup operator.
    stream = os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK), "rb")
    info = os.fstat(stream.fileno())
    if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1:
        stream.close()
        raise ValueError("Expected an unlinked regular file")
    return stream


def read_private(path, limit, env=False):
    with regular(path) as stream:
        info = os.fstat(stream.fileno())
        if info.st_uid != 0 or info.st_mode & (0o027 if env else 0o022):
            raise ValueError("Unsafe configuration ownership or permissions")
        if info.st_size > limit:
            raise ValueError("Configuration is too large")
        content = stream.read(limit + 1)
    if len(content) > limit:
        raise ValueError("Configuration is too large")
    return content


def validate_files(files):
    if not isinstance(files, dict) or not REQUIRED <= files.keys() or len(files) > 65:
        raise ValueError("Missing configuration or too many files")
    total = 0
    for name, content in files.items():
        if name != ".env" and not re.fullmatch(r"\.secrets/[A-Za-z0-9][A-Za-z0-9_.-]{0,127}", name):
            raise ValueError("Invalid configuration filename")
        if not isinstance(content, bytes) or len(content) > MAX_FILE_BYTES:
            raise ValueError("Invalid configuration size")
        total += len(content)
    if total > MAX_TOTAL_BYTES:
        raise ValueError("Configuration is too large")
    return files


def preflight(source):
    source = Path(source)
    directory(source)
    directory(source / ".secrets", private=True)
    files = {".env": read_private(source / ".env", MAX_FILE_BYTES, env=True)}
    for path in (source / ".secrets").iterdir():
        if len(files) >= 65:
            raise ValueError("Too many secret files")
        name = ".secrets/" + path.name
        if not re.fullmatch(r"\.secrets/[A-Za-z0-9][A-Za-z0-9_.-]{0,127}", name):
            raise ValueError("Invalid secret filename")
        files[name] = read_private(path, MAX_FILE_BYTES)
    return validate_files(files)


def data_hashes(backup):
    directory(backup)
    if not re.fullmatch(r"[0-9]{8}T[0-9]{6}Z", backup.name):
        raise ValueError("Invalid backup ID")
    datetime.strptime(backup.name, "%Y%m%dT%H%M%SZ")
    with regular(backup / "COMPLETE"):
        pass
    hashes = {}
    for name in ARTIFACTS:
        with regular(backup / name) as stream:
            hashes[name] = hashlib.file_digest(stream, "sha256").hexdigest()
    return hashes


def write_new(path, content, mode, dir_fd=None):
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, mode, dir_fd=dir_fd)
    with os.fdopen(descriptor, "wb") as stream:
        os.fchmod(stream.fileno(), mode)
        stream.write(content)
        stream.flush()
        os.fsync(stream.fileno())


def trusted_parent(target):
    """Open the restore destination's parent after checking every ancestor.

    Each ancestor must be a real, root-owned directory that other users cannot
    modify (a sticky world-writable directory such as /tmp is accepted because
    other users cannot rename or remove root's entries in it). Otherwise a
    lower-trust user could swap a directory between the checks and the writes.
    """
    parent = Path(os.path.realpath(target.parent))
    for ancestor in (parent, *parent.parents):
        info = ancestor.lstat()
        if not stat.S_ISDIR(info.st_mode) or info.st_uid != 0:
            raise ValueError("Restore destination ancestors must be root-owned directories")
        sticky = ancestor != parent and info.st_mode & stat.S_ISVTX
        if info.st_mode & 0o022 and not sticky:
            raise ValueError("Restore destination ancestors must not be writable by other users")
    descriptor = os.open(parent, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    info = os.fstat(descriptor)
    if info.st_uid != 0 or info.st_mode & 0o022:
        os.close(descriptor)
        raise ValueError("Unsafe restore destination parent")
    return descriptor


def private_child(parent_fd, name):
    """Create a root-private directory under parent_fd and return its descriptor."""
    os.mkdir(name, 0o700, dir_fd=parent_fd)
    descriptor = os.open(name, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=parent_fd)
    info = os.fstat(descriptor)
    if info.st_uid != 0 or info.st_mode & 0o077:
        os.close(descriptor)
        raise ValueError("Unexpected restore directory ownership or permissions")
    return descriptor


def capture(source, backup):
    backup = Path(backup)
    files = preflight(source)
    bundle = {
        "version": 1, "backup_id": backup.name, "sha256": data_hashes(backup),
        "files": {name: base64.b64encode(value).decode("ascii") for name, value in files.items()},
    }
    private = backup / "installation"
    private.mkdir(mode=0o700)
    temporary = private / ".configuration.tmp"
    checksum = private / "configuration.sha256"
    try:
        raw = json.dumps(bundle, sort_keys=True).encode("ascii")
        write_new(temporary, raw, 0o600)
        write_new(checksum, (hashlib.sha256(raw).hexdigest() + "\n").encode("ascii"), 0o600)
        os.replace(temporary, private / "configuration.json")
        descriptor = os.open(private, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        try:
            os.fsync(descriptor)
        finally:
            os.close(descriptor)
    except BaseException:
        temporary.unlink(missing_ok=True)
        checksum.unlink(missing_ok=True)
        (private / "configuration.json").unlink(missing_ok=True)
        private.rmdir()
        raise


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("Duplicate configuration field")
        result[key] = value
    return result


def verify(backup):
    backup = Path(backup)
    hashes = data_hashes(backup)
    directory(backup / "installation", private=True)
    raw = read_private(backup / "installation/configuration.json", MAX_BUNDLE_BYTES, env=True)
    checksum = read_private(backup / "installation/configuration.sha256", 65, env=True)
    if checksum != (hashlib.sha256(raw).hexdigest() + "\n").encode("ascii"):
        raise ValueError("Configuration checksum does not match")
    try:
        bundle = json.loads(raw, object_pairs_hook=unique_object)
    except (UnicodeError, RecursionError) as error:
        raise ValueError("Invalid configuration JSON") from error
    if not isinstance(bundle, dict) or set(bundle) != {"version", "backup_id", "sha256", "files"}:
        raise ValueError("Invalid configuration fields")
    if type(bundle["version"]) is not int or bundle["version"] != 1:
        raise ValueError("Unsupported configuration version")
    if bundle["backup_id"] != backup.name or bundle["sha256"] != hashes:
        raise ValueError("Configuration does not match this data backup")
    encoded = bundle["files"]
    if not isinstance(encoded, dict) or len(encoded) > 65:
        raise ValueError("Invalid configuration files")
    files = {}
    for name, value in encoded.items():
        if not isinstance(value, str) or len(value) > 4 * ((MAX_FILE_BYTES + 2) // 3):
            raise ValueError("Invalid encoded configuration")
        try:
            files[name] = base64.b64decode(value, validate=True)
        except (ValueError, binascii.Error) as error:
            raise ValueError("Invalid encoded configuration") from error
    return validate_files(files)


def restore(backup, target):
    files = verify(backup)
    target = Path(target)
    if target.name in ("", ".", ".."):
        raise ValueError("Invalid restore destination")
    # Every write below is relative to a retained directory descriptor, so
    # replacing a pathname component after this point cannot redirect secrets.
    parent_fd = trusted_parent(target)
    descriptors = [parent_fd]
    created = []
    target_created = secrets_created = False
    try:
        target_fd = private_child(parent_fd, target.name)
        target_created = True
        descriptors.append(target_fd)
        secrets_fd = private_child(target_fd, ".secrets")
        secrets_created = True
        descriptors.append(secrets_fd)
        for name, value in files.items():
            if name == ".env":
                directory_fd, filename, mode = target_fd, ".env", 0o600
            else:
                directory_fd, filename, mode = secrets_fd, name.removeprefix(".secrets/"), 0o644
            write_new(filename, value, mode, dir_fd=directory_fd)
            created.append((directory_fd, filename))
        for descriptor in reversed(descriptors):
            os.fsync(descriptor)
    except BaseException:
        for directory_fd, filename in reversed(created):
            try:
                os.unlink(filename, dir_fd=directory_fd)
            except FileNotFoundError:
                pass
        if secrets_created:
            os.rmdir(".secrets", dir_fd=descriptors[1])
        if target_created:
            os.rmdir(target.name, dir_fd=parent_fd)
        raise
    finally:
        for descriptor in reversed(descriptors):
            os.close(descriptor)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="command", required=True)
    sub.add_parser("preflight").add_argument("source", type=Path)
    capture_args = sub.add_parser("capture")
    capture_args.add_argument("source", type=Path)
    capture_args.add_argument("backup", type=Path)
    sub.add_parser("verify").add_argument("backup", type=Path)
    restore_args = sub.add_parser("restore")
    restore_args.add_argument("backup", type=Path)
    restore_args.add_argument("target", type=Path)
    args = parser.parse_args()
    if not hasattr(os, "geteuid") or os.geteuid() != 0:
        parser.exit(1, "Installation backup operations require root on Linux.\n")
    os.umask(0o077)
    try:
        if args.command == "preflight":
            preflight(args.source)
        elif args.command == "capture":
            capture(args.source, args.backup)
        elif args.command == "verify":
            verify(args.backup)
        else:
            restore(args.backup, args.target)
    except (OSError, ValueError):
        parser.exit(1, "Installation backup failed: check paths, permissions and backup integrity.\n")
    print("Installation configuration " + args.command + " succeeded.")


if __name__ == "__main__":
    main()

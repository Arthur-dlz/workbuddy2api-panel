#!/usr/bin/env python3
"""Atomically persist the login.sh auth record with owner-only permissions."""

import json
import os
import sys
import tempfile
from pathlib import Path


def auth_from_environment():
    return {
        "account": {
            "uid": os.environ["WB2A_LOGIN_USER_ID"],
            "enterpriseId": os.environ["WB2A_LOGIN_ENT_ID"],
            "nickname": os.environ["WB2A_LOGIN_NICKNAME"],
        },
        "auth": {
            "accessToken": os.environ["WB2A_LOGIN_TOKEN"],
            "refreshToken": os.environ["WB2A_LOGIN_REFRESH"],
            "expiresAt": int(os.environ["WB2A_LOGIN_EXPIRES_AT"]),
            "domain": os.environ["WB2A_LOGIN_DOMAIN"],
        },
    }


def write_auth_file(path, record):
    target = Path(path)
    target.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    if os.name == "posix":
        # mkdir(mode=...) does not change permissions when the directory already exists.
        target.parent.chmod(0o700)
    fd, temporary = tempfile.mkstemp(prefix=f".{target.name}.", suffix=".tmp", dir=target.parent)
    try:
        if hasattr(os, "fchmod"):
            os.fchmod(fd, 0o600)
        with os.fdopen(fd, "w", encoding="utf-8") as stream:
            fd = -1
            json.dump(record, stream, indent=1)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, target)
        temporary = None
        if hasattr(os, "O_DIRECTORY"):
            dir_fd = os.open(target.parent, os.O_RDONLY | os.O_DIRECTORY)
            try:
                os.fsync(dir_fd)
            finally:
                os.close(dir_fd)
    finally:
        if fd >= 0:
            os.close(fd)
        if temporary is not None:
            try:
                os.unlink(temporary)
            except FileNotFoundError:
                pass


def main():
    target = Path(sys.argv[1])
    write_auth_file(target, auth_from_environment())
    action = os.environ["WB2A_LOGIN_ACTION"]
    print(f"已保存（{action}）: {target}")


if __name__ == "__main__":
    main()

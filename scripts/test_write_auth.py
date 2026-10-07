import importlib.util
import json
import os
import stat
import tempfile
import unittest
from pathlib import Path
from unittest import mock


MODULE_PATH = Path(__file__).with_name("write_auth.py")
SPEC = importlib.util.spec_from_file_location("write_auth", MODULE_PATH)
write_auth = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(write_auth)


class WriteAuthTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.path = Path(self.temp.name) / "auths" / "workbuddy-fixture.json"
        self.path.parent.mkdir(mode=0o700)
        self.record = {
            "account": {"uid": "fixture", "enterpriseId": "ent", "nickname": "Fixture"},
            "auth": {"accessToken": "access-fixture", "refreshToken": "refresh-fixture", "expiresAt": 123, "domain": "workbuddy.cn"},
        }

    def test_atomic_write_preserves_nested_schema_and_replaces_existing(self):
        self.path.write_text('{"old":true}', encoding="utf-8")
        write_auth.write_auth_file(self.path, self.record)
        self.assertEqual(json.loads(self.path.read_text(encoding="utf-8")), self.record)
        self.assertEqual(list(self.path.parent.glob(".*.tmp")), [])

    @unittest.skipUnless(os.name == "posix", "POSIX permission bits are not available")
    def test_posix_file_mode_is_restricted_under_permissive_umask(self):
        previous = os.umask(0o022)
        try:
            write_auth.write_auth_file(self.path, self.record)
            self.assertEqual(stat.S_IMODE(self.path.stat().st_mode), 0o600)
            write_auth.write_auth_file(self.path, {**self.record, "replacement": True})
            self.assertEqual(stat.S_IMODE(self.path.stat().st_mode), 0o600)
            self.assertEqual(stat.S_IMODE(self.path.parent.stat().st_mode), 0o700)
        finally:
            os.umask(previous)

    @unittest.skipUnless(os.name == "posix", "POSIX permission bits are not available")
    def test_posix_existing_auth_dir_is_restricted_under_permissive_umask(self):
        self.path.parent.chmod(0o755)
        previous = os.umask(0o022)
        try:
            write_auth.write_auth_file(self.path, self.record)
            self.assertEqual(stat.S_IMODE(self.path.stat().st_mode), 0o600)
            self.assertEqual(stat.S_IMODE(self.path.parent.stat().st_mode), 0o700)
        finally:
            os.umask(previous)

    def test_replace_failure_keeps_existing_file_and_cleans_temporary_file(self):
        self.path.write_text('{"old":true}', encoding="utf-8")
        with mock.patch.object(write_auth.os, "replace", side_effect=OSError("injected replace failure")):
            with self.assertRaisesRegex(OSError, "injected replace failure"):
                write_auth.write_auth_file(self.path, self.record)
        self.assertEqual(self.path.read_text(encoding="utf-8"), '{"old":true}')
        self.assertEqual(list(self.path.parent.glob(".*.tmp")), [])


if __name__ == "__main__":
    unittest.main()

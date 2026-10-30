"""Exercise fixture privacy with real permissions and synthetic file contents."""

import os
from pathlib import Path
import subprocess
import tempfile
import unittest

from fixture_permissions import require_private_file


class FixturePermissionsTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="forcad-acl-test-")
        self.addCleanup(self.temporary.cleanup)
        self.directory = Path(self.temporary.name)

    def windows_acl(self, path, *, directory=False, public=False):
        script = r"""$ErrorActionPreference = 'Stop'
$user = [Security.Principal.WindowsIdentity]::GetCurrent().User
$flags = if ($env:FORCAD_TEST_ACL_DIRECTORY -eq '1') { 'ContainerInherit,ObjectInherit' } else { 'None' }
$acl = if ($env:FORCAD_TEST_ACL_DIRECTORY -eq '1') {
    New-Object Security.AccessControl.DirectorySecurity
} else {
    New-Object Security.AccessControl.FileSecurity
}
$acl.SetOwner($user)
$acl.SetAccessRuleProtection($true, $false)
foreach ($sid in (@($user.Value, 'S-1-5-18', 'S-1-5-32-544') | Select-Object -Unique)) {
    $identity = New-Object Security.Principal.SecurityIdentifier($sid)
    $rule = New-Object Security.AccessControl.FileSystemAccessRule($identity, 'FullControl', $flags, 'None', 'Allow')
    $acl.AddAccessRule($rule)
}
if ($env:FORCAD_TEST_ACL_PUBLIC -eq '1') {
    $everyone = New-Object Security.Principal.SecurityIdentifier('S-1-1-0')
    $rule = New-Object Security.AccessControl.FileSystemAccessRule($everyone, 'Read', $flags, 'None', 'Allow')
    $acl.AddAccessRule($rule)
}
if ($env:FORCAD_TEST_ACL_DIRECTORY -eq '1') {
    [IO.Directory]::SetAccessControl($env:FORCAD_TEST_ACL_PATH, $acl)
} else {
    [IO.File]::SetAccessControl($env:FORCAD_TEST_ACL_PATH, $acl)
}
"""
        result = subprocess.run(
            ["powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script],
            env={**os.environ, "FORCAD_TEST_ACL_PATH": str(path),
                 "FORCAD_TEST_ACL_DIRECTORY": str(int(directory)), "FORCAD_TEST_ACL_PUBLIC": str(int(public))},
            capture_output=True,
            timeout=10,
            check=False,
        )
        self.assertEqual(result.returncode, 0, result.stderr.decode(errors="replace"))

    def test_private_file_and_inherited_private_acl(self):
        if os.name == "nt":
            self.windows_acl(self.directory, directory=True)
        path = self.directory / "sentinel.txt"
        path.write_text("synthetic-fixture-value", encoding="utf-8")
        if os.name != "nt":
            path.chmod(0o600)
        require_private_file(path)
        if os.name == "nt":
            self.windows_acl(path)
            require_private_file(path)

    def test_public_read_access_is_rejected(self):
        path = self.directory / "sentinel.txt"
        path.write_text("synthetic-fixture-value", encoding="utf-8")
        if os.name == "nt":
            self.windows_acl(path, public=True)
        else:
            path.chmod(0o644)
        with self.assertRaises(RuntimeError):
            require_private_file(path)

    def test_directory_and_missing_path_are_rejected(self):
        for path in (self.directory, self.directory / "missing"):
            with self.subTest(path=path.name), self.assertRaises(RuntimeError):
                require_private_file(path)


if __name__ == "__main__":
    unittest.main()

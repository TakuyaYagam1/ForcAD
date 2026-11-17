"""Check private deployment fixture files without printing their contents."""

import os
import stat
import subprocess
from pathlib import Path


WINDOWS_PRIVATE_FILE_CHECK = r"""$ErrorActionPreference = 'Stop'
try {
    $file = Get-Item -LiteralPath $env:FORCAD_TEST_PRIVATE_FILE
    if ($file.PSIsContainer -or ($file.Attributes -band [IO.FileAttributes]::ReparsePoint)) {
        throw 'Expected an ordinary fixture file'
    }
    $user = [Security.Principal.WindowsIdentity]::GetCurrent().User.Value
    $allowed = @($user, 'S-1-5-18', 'S-1-5-32-544') | Select-Object -Unique
    $acl = Get-Acl -LiteralPath $file.FullName
    $rules = @($acl.GetAccessRules($true, $true, [Security.Principal.SecurityIdentifier]))
    if ($rules.Count -ne $allowed.Count) { throw 'Unexpected ACL entry count' }
    $seen = New-Object 'Collections.Generic.HashSet[string]'
    foreach ($rule in $rules) {
        if ($rule.IdentityReference.Value -notin $allowed -or
            $rule.AccessControlType -ne 'Allow' -or
            $rule.FileSystemRights -ne 'FullControl' -or
            -not $seen.Add($rule.IdentityReference.Value)) {
            throw 'Unexpected fixture file access'
        }
    }
} catch {
    [Console]::Error.WriteLine('Runtime credential ACL verification failed')
    exit 1
}
"""


def require_private_file(path):
    path = Path(path)
    try:
        info = path.lstat()
        if not stat.S_ISREG(info.st_mode):
            raise ValueError("Expected an ordinary fixture file")
        if os.name == "nt":
            result = subprocess.run(
                ["powershell.exe", "-NoProfile", "-NonInteractive", "-Command", WINDOWS_PRIVATE_FILE_CHECK],
                env={**os.environ, "FORCAD_TEST_PRIVATE_FILE": str(path.absolute())},
                capture_output=True,
                timeout=10,
                check=False,
            )
            if result.returncode:
                raise ValueError("Fixture ACL is not private")
        elif info.st_mode & 0o077:
            raise ValueError("Fixture mode is not private")
    except (OSError, ValueError, subprocess.TimeoutExpired) as error:
        raise RuntimeError("Runtime credentials must be a verified private file") from error

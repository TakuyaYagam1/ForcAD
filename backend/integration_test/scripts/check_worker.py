"""Check the installed worker runtime without contacting participant services."""

import ctypes
import os
import ssl
import subprocess
import sys
import tempfile
import unittest

import certifi
import cffi
import checklib
import portalocker
import requests
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey


class WorkerRuntimeTest(unittest.TestCase):
    def test_python_dependencies(self):
        self.assertEqual(os.getuid(), 10001)
        self.assertEqual(sys.prefix, "/opt/checkers")
        self.assertTrue(callable(checklib.BaseChecker))
        self.assertEqual(requests.Request("GET", "http://localhost/").prepare().method, "GET")
        context = ssl.create_default_context(cafile=certifi.where())
        self.assertGreater(context.cert_store_stats()["x509_ca"], 0)

    def test_native_extensions(self):
        ffi = cffi.FFI()
        ffi.cdef("int getpid(void);")
        self.assertEqual(ffi.dlopen(None).getpid(), os.getpid())
        self.assertGreater(ctypes.CDLL("libpq.so.5").PQlibVersion(), 0)
        key = Ed25519PrivateKey.generate()
        message = b"checker runtime"
        key.public_key().verify(key.sign(message), message)
        with tempfile.TemporaryFile() as handle:
            portalocker.lock(handle, portalocker.LOCK_EX | portalocker.LOCK_NB)
            portalocker.unlock(handle)

    def test_memory_limit(self):
        source = """
import resource
assert resource.getrlimit(resource.RLIMIT_AS) == (128 << 20, 128 << 20)
try:
    bytearray(256 << 20)
except MemoryError:
    pass
else:
    raise AssertionError('checker allocation exceeded its memory limit')
"""
        subprocess.run(
            ["prlimit", f"--as={128 << 20}", "--", sys.executable, "-c", source],
            check=True, timeout=10,
        )


if __name__ == "__main__":
    unittest.main(verbosity=2)

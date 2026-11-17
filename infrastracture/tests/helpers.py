"""Load role implementations against rendered bundle fixtures."""

import importlib
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))

from hostlib import checks, common, files

__all__ = ["checks", "common", "files", "small_config", "source_bundle"]


def small_config(config, count=2, members=2, admins=1):
    config["teams"]["entries"] = config["teams"]["entries"][:count]
    config["teams"]["members_per_team"] = members
    config["admins"] = admins
    return config


def source_bundle(role, directory, root=Path("/")):
    """Use source role code with the supplied bundle data and test root."""
    role_path = str(ROOT / role)
    if role_path not in sys.path:
        sys.path.insert(0, role_path)
    module = importlib.import_module(f"{role}.host")
    bundle = module.Bundle(directory, root=root)
    # File transaction fixtures must never invoke host runtime operations.
    bundle.files_only = root != Path("/")
    return bundle

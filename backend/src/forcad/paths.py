"""Project path discovery for installed and source-tree commands."""

from __future__ import annotations

import os
from pathlib import Path


def resolve_project_root() -> Path:
    """Resolve the workspace root without depending on the current directory.

    Installed commands should set ``FORCAD_ROOT``. For a source checkout, walk
    up from the working directory and package location. If neither identifies a
    checkout, use the current directory as a safe fallback for help and local
    operator commands.
    """
    configured = os.environ.get('FORCAD_ROOT')
    if configured:
        root = Path(configured).expanduser()
        if not root.is_absolute():
            root = Path.cwd() / root
        return root.resolve()

    package_checkout = Path(__file__).resolve().parents[3]
    working_directory = Path.cwd().resolve()
    candidates = [working_directory, *working_directory.parents]
    candidates.extend([package_checkout, *package_checkout.parents])
    seen = set()
    for candidate in candidates:
        if candidate in seen:
            continue
        seen.add(candidate)
        has_compose = (
            candidate / 'deployment' / 'docker' / 'compose.yml'
        ).is_file()
        has_package = (candidate / 'backend' / 'pyproject.toml').is_file()
        if has_compose or has_package:
            return candidate

    return Path.cwd().resolve()

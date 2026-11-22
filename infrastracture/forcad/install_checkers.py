"""Install the private bundle's checker files for the non-root worker."""

import shutil
import sys
from pathlib import Path

from hostlib.common import read_yaml


def install_checkers(bundle: Path, target: Path) -> None:
    source = bundle / "checkers"
    tasks = read_yaml(bundle / "config.yml")["tasks"]
    entrypoints = {task["checker"] for task in tasks}

    def copy(src: Path, dst: Path) -> None:
        if src.is_symlink() or dst.is_symlink():
            raise ValueError(f"Refusing symlink at {src.relative_to(bundle)}")
        if src.is_dir():
            dst.mkdir(exist_ok=True)
            dst.chmod(0o755)
            for child in src.iterdir():
                copy(child, dst / child.name)
            return
        if not src.is_file():
            raise ValueError(f"Not a regular checker file: {src}")
        name = src.relative_to(source).as_posix()
        # Replace checkout files rather than changing a shared inode's mode.
        dst.unlink(missing_ok=True)
        shutil.copyfile(src, dst)
        dst.chmod(0o755 if name in entrypoints else 0o644)
        entrypoints.discard(name)

    copy(source, target / "checkers")
    if entrypoints:
        raise ValueError(f"Missing bundled checker: {min(entrypoints)}")


if __name__ == "__main__":
    install_checkers(Path(__file__).resolve().parent, Path(sys.argv[1]))

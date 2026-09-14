#!/usr/bin/env python3
"""Opt this checkout into the tracked hooks without replacing existing hooks."""
from pathlib import Path
import os
import subprocess
import sys


def install():
    root = Path(__file__).resolve().parent.parent
    actual = Path(subprocess.check_output(['git', 'rev-parse', '--show-toplevel'], cwd=root, text=True).strip()).resolve()
    if actual != root:
        raise RuntimeError('Run the installer from a Sama Git checkout.')
    result = subprocess.run(['git', 'config', '--get', 'core.hooksPath'], cwd=root, capture_output=True, text=True, check=False)
    if result.returncode not in (0, 1):
        raise RuntimeError('Cannot inspect existing hooks configuration.')
    existing = result.stdout.strip()
    if existing and existing != '.githooks':
        raise RuntimeError(f'Existing core.hooksPath is {existing!r}; integrate deliberately instead of replacing it.')
    old = Path(subprocess.check_output(['git', 'rev-parse', '--git-path', 'hooks'], cwd=root, text=True).strip())
    if not old.is_absolute():
        old = root / old
    if not existing and old.is_dir():
        active = [p.name for p in old.iterdir() if not p.name.endswith('.sample') and p.is_file() and os.access(p, os.X_OK)]
        if active:
            raise RuntimeError('Existing executable hooks found: ' + ', '.join(active))
    hook = root / '.githooks/pre-commit'
    if hook.is_symlink() or not hook.is_file() or not os.access(hook, os.X_OK):
        raise RuntimeError('The tracked pre-commit hook must be a regular executable file.')
    subprocess.run(['git', 'config', '--local', 'core.hooksPath', '.githooks'], cwd=root, check=True)
    print('Installed .githooks for this checkout. Run the installer after each fresh clone.')


if __name__ == '__main__':
    try:
        install()
    except (OSError, RuntimeError, subprocess.SubprocessError) as error:
        print(f'Hook installation failed: {error}', file=sys.stderr)
        sys.exit(1)

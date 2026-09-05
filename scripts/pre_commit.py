#!/usr/bin/env python3
"""Check the staged tree without rewriting the index or the working tree."""
import os
from pathlib import Path
import subprocess
import sys
import tempfile


def run(args, cwd, **kwargs):
    return subprocess.run(args, cwd=cwd, check=True, timeout=600, **kwargs)


def main():
    root = Path(subprocess.check_output(['git', 'rev-parse', '--show-toplevel'], text=True).strip())
    if subprocess.check_output(['git', 'ls-files', '--unmerged'], cwd=root):
        raise RuntimeError('Resolve merge conflicts before committing.')
    entries = subprocess.check_output(['git', 'ls-files', '--stage', '-z'], cwd=root).split(b'\0')
    for entry in filter(None, entries):
        metadata, raw_path = entry.split(b'\t', 1)
        mode = metadata.split()[0]
        path = os.fsdecode(raw_path)
        if mode not in (b'100644', b'100755'):
            raise RuntimeError(f'Staged symlinks/submodules are not supported by this check: {path}')
        if path.startswith(('agents/', 'frontend/node_modules/', 'frontend/dist/', 'backend/bin/')):
            raise RuntimeError(f'Local/generated files must not be staged: {path}')

    # Local collaboration evidence is deliberately absent from exported Git content.
    run([sys.executable, 'scripts/collab.py', 'lint', '.'], root)
    with tempfile.TemporaryDirectory(prefix='sama-staged-') as temporary:
        snapshot = Path(temporary)
        run(['git', 'checkout-index', '--all', f'--prefix={snapshot}{os.sep}'], root)
        required = ['backend/scripts/check.sh', 'frontend/package.json', 'frontend/pnpm-lock.yaml']
        for relative in required:
            if not (snapshot / relative).is_file():
                raise RuntimeError(f'Stage the complete scaffold first; missing {relative}')
        # Git hook variables refer to the original index/repository, not temporary
        # snapshots or the disposable Git repositories created by tool tests.
        check_env = dict(os.environ)
        local_vars = subprocess.check_output(['git', 'rev-parse', '--local-env-vars'], cwd=root, text=True).splitlines()
        for key in local_vars:
            check_env.pop(key, None)
        print('Checking staged backend…', flush=True)
        run(['sh', 'scripts/check.sh'], snapshot / 'backend', env=check_env)
        print('Checking staged backoffice…', flush=True)
        # Resolve the source project's store before crossing into a temporary
        # directory, which may be on another volume with a different default store.
        store = run(['pnpm', 'store', 'path'], root / 'frontend', env=check_env,
                    capture_output=True, text=True).stdout.strip()
        if not store or not Path(store).is_absolute():
            raise RuntimeError('pnpm did not report an absolute store path.')
        # Dependencies come from the staged lockfile, never the working node_modules.
        run(['pnpm', 'install', '--frozen-lockfile', '--offline', '--ignore-scripts',
             '--prod=false', '--store-dir', store], snapshot / 'frontend', env=check_env)
        run(['pnpm', 'run', 'check'], snapshot / 'frontend', env=check_env)
        run([sys.executable, '-B', '-m', 'unittest', 'discover', '-s', 'scripts/tests'], snapshot, env=check_env)
    print('Staged checks passed; index and working files were not modified.')


if __name__ == '__main__':
    try:
        main()
    except (OSError, RuntimeError, subprocess.SubprocessError) as error:
        print(f'Pre-commit check failed: {error}', file=sys.stderr)
        print('Use the pinned toolchains. For missing pnpm store entries, run '
              'pnpm install --frozen-lockfile --ignore-scripts in frontend. '
              'Fix reported issues, format if needed, and stage the intended changes again.', file=sys.stderr)
        sys.exit(1)

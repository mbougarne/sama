#!/usr/bin/env python3
"""Check the staged tree without rewriting the index or the working tree."""
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile


def run(args, cwd, **kwargs):
    return subprocess.run(args, cwd=cwd, check=True, timeout=600, **kwargs)


def pinned_frontend_versions(frontend):
    node_version = (frontend / '.nvmrc').read_text().strip()
    if not re.fullmatch(r'v?\d+\.\d+\.\d+', node_version):
        raise RuntimeError('Staged frontend/.nvmrc must contain one exact Node version.')
    node_version = node_version.removeprefix('v')
    try:
        manifest = json.loads((frontend / 'package.json').read_text())
    except json.JSONDecodeError as error:
        raise RuntimeError('Staged frontend/package.json is not valid JSON.') from error
    engines = manifest.get('engines') if isinstance(manifest, dict) else None
    if not isinstance(engines, dict) or engines.get('node') != node_version:
        raise RuntimeError('Staged frontend/.nvmrc and package.json engines.node must match exactly.')
    manager = manifest.get('packageManager', '')
    match = re.fullmatch(r'pnpm@(\d+\.\d+\.\d+)(?:\+.+)?', manager)
    if not match:
        raise RuntimeError('Staged frontend/package.json must pin an exact pnpm packageManager version.')
    return node_version, match.group(1)


def version_of(command, cwd, env):
    try:
        return run(command, cwd, env=env, capture_output=True, text=True).stdout.strip()
    except (OSError, subprocess.SubprocessError):
        return None


def frontend_toolchain(frontend, base_env):
    node_version, pnpm_version = pinned_frontend_versions(frontend)
    env = dict(base_env)
    current_node = version_of(['node', '--version'], frontend, env)
    if current_node != f'v{node_version}':
        nvm_root = Path(env.get('NVM_DIR', Path.home() / '.nvm')).expanduser().resolve()
        node_bin = nvm_root / 'versions' / 'node' / f'v{node_version}' / 'bin'
        node = node_bin / 'node'
        installed_node = version_of([str(node), '--version'], frontend, env) if node.is_file() else None
        if installed_node != f'v{node_version}':
            actual = current_node or 'unavailable'
            raise RuntimeError(
                f'Staged frontend requires Node v{node_version}, but current Node is {actual}. '
                f'The matching nvm installation was not found. Run `nvm install {node_version}` once, '
                'or activate that exact version with another Node manager before committing.'
            )
        env['PATH'] = str(node_bin) + os.pathsep + env.get('PATH', '')
        print(f'Using staged Node v{node_version} from the installed nvm toolchain.', flush=True)

    pnpm = shutil.which('pnpm', path=env.get('PATH'))
    if not pnpm:
        raise RuntimeError(f'pnpm {pnpm_version} is required but was not found on PATH.')
    actual_pnpm = version_of([pnpm, '--version'], frontend, env)
    if actual_pnpm != pnpm_version:
        raise RuntimeError(
            f'Staged frontend requires pnpm {pnpm_version}, but the selected pnpm is '
            f'{actual_pnpm or "unavailable"}. Enable the packageManager version before committing.'
        )
    return env, pnpm


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
        required = [
            'backend/scripts/check.sh',
            'frontend/.nvmrc',
            'frontend/package.json',
            'frontend/pnpm-lock.yaml',
        ]
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
        frontend_env, pnpm = frontend_toolchain(snapshot / 'frontend', check_env)
        # Resolve the source project's store before crossing into a temporary
        # directory, which may be on another volume with a different default store.
        store = run([pnpm, 'store', 'path'], root / 'frontend', env=frontend_env,
                    capture_output=True, text=True).stdout.strip()
        if not store or not Path(store).is_absolute():
            raise RuntimeError('pnpm did not report an absolute store path.')
        # Dependencies come from the staged lockfile, never the working node_modules.
        run([pnpm, 'install', '--frozen-lockfile', '--offline', '--ignore-scripts',
             '--prod=false', '--store-dir', store], snapshot / 'frontend', env=frontend_env)
        run([pnpm, 'run', 'check'], snapshot / 'frontend', env=frontend_env)
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

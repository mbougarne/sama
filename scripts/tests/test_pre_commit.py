"""Exercise real Git staging with small deterministic stand-ins for app checks."""
from pathlib import Path
import json
import os
import shutil
import subprocess
import sys
import tempfile
import unittest

SCRIPTS = Path(__file__).resolve().parents[1]


class PreCommitTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name) / 'repo'
        self.root.mkdir()
        subprocess.run(['git', 'init', '-q', str(self.root)], check=True)
        for folder in ('scripts/tests', 'backend/scripts', 'frontend', '.githooks'):
            (self.root / folder).mkdir(parents=True, exist_ok=True)
        for name in ('collab.py', 'pre_commit.py', 'install_hooks.py'):
            shutil.copyfile(SCRIPTS / name, self.root / 'scripts' / name)
        shutil.copytree(SCRIPTS / 'agent_collaboration', self.root / 'scripts/agent_collaboration')
        (self.root / 'scripts/tests/test_snapshot.py').write_text(
            'import unittest\nfrom pathlib import Path\n'
            'class Snapshot(unittest.TestCase):\n'
            '    def test_export(self):\n'
            '        self.assertEqual(Path("backend/source.txt").read_text(), "good\\n")\n'
        )
        (self.root / '.gitignore').write_text('/agents/\n')
        (self.root / 'backend/scripts/check.sh').write_text('#!/bin/sh\nset -eu\ntest "$(cat source.txt)" = good\n')
        (self.root / 'backend/source.txt').write_text('good\n')
        (self.root / 'frontend/package.json').write_text('{}\n')
        (self.root / 'frontend/pnpm-lock.yaml').write_text('{}\n')
        hook = self.root / '.githooks/pre-commit'
        hook.write_text('#!/bin/sh\nexit 0\n')
        hook.chmod(0o755)
        self.tools = Path(self.temp.name) / 'tools'
        self.tools.mkdir()
        pnpm = self.tools / 'pnpm'
        pnpm.write_text(
            f'#!{sys.executable}\n'
            'import json, os, sys\nfrom pathlib import Path\n'
            'args = sys.argv[1:]\n'
            'with open(os.environ["PNPM_TEST_CALLS"], "a") as calls:\n'
            '    calls.write(json.dumps({"args": args, "cwd": os.getcwd()}) + "\\n")\n'
            'if args == ["store", "path"]:\n'
            '    print(os.environ["PNPM_TEST_STORE"])\n'
            'elif args and args[0] == "install":\n'
            '    assert Path("pnpm-lock.yaml").is_file()\n'
            '    assert not Path("package-lock.json").exists()\n'
            '    sys.exit(int(os.environ.get("PNPM_TEST_FAIL_INSTALL", "0")))\n'
            'elif args != ["run", "check"]:\n'
            '    sys.exit(2)\n'
        )
        pnpm.chmod(0o755)
        self.env = dict(os.environ, PATH=str(self.tools) + os.pathsep + os.environ['PATH'])
        self.calls = Path(self.temp.name) / 'pnpm-calls.jsonl'
        self.env['PNPM_TEST_CALLS'] = str(self.calls)
        self.env['PNPM_TEST_STORE'] = str(Path(self.temp.name) / 'shared store/v10')
        self.git('add', '.')

    def git(self, *args):
        return subprocess.check_output(['git', *args], cwd=self.root)

    def check(self):
        return subprocess.run([sys.executable, 'scripts/pre_commit.py'], cwd=self.root,
                              env=self.env, capture_output=True, text=True, timeout=30)

    def test_checks_staged_bytes_and_preserves_unstaged_edit(self):
        path = self.root / 'backend/source.txt'
        path.write_text('bad unstaged edit\n')
        index = self.git('ls-files', '--stage')
        result = self.check()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(path.read_text(), 'bad unstaged edit\n')
        self.assertEqual(self.git('ls-files', '--stage'), index)

    def test_rejects_bad_staged_bytes_despite_fixed_worktree(self):
        path = self.root / 'backend/source.txt'
        path.write_text('bad\n'); self.git('add', 'backend/source.txt')
        path.write_text('good\n')
        result = self.check()
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.git('show', ':backend/source.txt'), b'bad\n')
        self.assertEqual(path.read_text(), 'good\n')

    def test_rejects_force_added_private_records(self):
        (self.root / 'agents').mkdir()
        (self.root / 'agents/private.md').write_text('local context\n')
        self.git('add', '-f', 'agents/private.md')
        self.assertIn('must not be staged', self.check().stderr)

    def test_uses_frozen_pnpm_lock_and_source_store_in_snapshot(self):
        result = self.check()
        self.assertEqual(result.returncode, 0, result.stderr)
        calls = [json.loads(line) for line in self.calls.read_text().splitlines()]
        self.assertEqual(calls[0], {'args': ['store', 'path'], 'cwd': str((self.root / 'frontend').resolve())})
        self.assertEqual(calls[1]['args'], [
            'install', '--frozen-lockfile', '--offline', '--ignore-scripts',
            '--prod=false', '--store-dir', self.env['PNPM_TEST_STORE'],
        ])
        self.assertNotEqual(calls[1]['cwd'], str((self.root / 'frontend').resolve()))
        self.assertEqual(calls[2], {'args': ['run', 'check'], 'cwd': calls[1]['cwd']})

    def test_failed_pnpm_install_blocks_checks_and_preserves_index(self):
        self.env['PNPM_TEST_FAIL_INSTALL'] = '1'
        index = self.git('ls-files', '--stage')
        result = self.check()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('pnpm install --frozen-lockfile', result.stderr)
        calls = [json.loads(line) for line in self.calls.read_text().splitlines()]
        self.assertEqual(len(calls), 2)
        self.assertEqual(self.git('ls-files', '--stage'), index)

    def test_unstaged_lockfile_cannot_replace_missing_staged_lockfile(self):
        self.git('rm', '--cached', 'frontend/pnpm-lock.yaml')
        result = self.check()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('missing frontend/pnpm-lock.yaml', result.stderr)
        self.assertTrue((self.root / 'frontend/pnpm-lock.yaml').is_file())
        self.assertFalse(self.calls.exists())

    def test_installer_is_idempotent(self):
        command = [sys.executable, 'scripts/install_hooks.py']
        for _ in range(2):
            result = subprocess.run(command, cwd=self.root, capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.git('config', '--local', '--get', 'core.hooksPath'), b'.githooks\n')

    def test_installer_preserves_existing_hook_path(self):
        self.git('config', '--local', 'core.hooksPath', '.existing-hooks')
        result = subprocess.run([sys.executable, 'scripts/install_hooks.py'], cwd=self.root, capture_output=True, text=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.git('config', '--local', '--get', 'core.hooksPath'), b'.existing-hooks\n')

    def test_installer_preserves_existing_default_hook(self):
        existing = self.root / '.git/hooks/pre-commit'
        existing.write_text('#!/bin/sh\nexit 7\n'); existing.chmod(0o755)
        result = subprocess.run([sys.executable, 'scripts/install_hooks.py'], cwd=self.root, capture_output=True, text=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(existing.read_text(), '#!/bin/sh\nexit 7\n')


if __name__ == '__main__':
    unittest.main()

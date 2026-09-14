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
        (self.root / 'frontend/.nvmrc').write_text('24.20.0\n')
        (self.root / 'frontend/package.json').write_text(
            '{"engines":{"node":"24.20.0"},"packageManager":"pnpm@10.6.5"}\n'
        )
        (self.root / 'frontend/pnpm-lock.yaml').write_text('{}\n')
        hook = self.root / '.githooks/pre-commit'
        hook.write_text('#!/bin/sh\nexit 0\n')
        hook.chmod(0o755)
        self.tools = Path(self.temp.name) / 'tools'
        self.tools.mkdir()
        node = self.tools / 'node'
        node.write_text('#!/bin/sh\nprintf "v24.20.0\\n"\n')
        node.chmod(0o755)
        pnpm = self.tools / 'pnpm'
        pnpm.write_text(
            f'#!{sys.executable}\n'
            'import json, os, subprocess, sys\nfrom pathlib import Path\n'
            'args = sys.argv[1:]\n'
            'with open(os.environ["PNPM_TEST_CALLS"], "a") as calls:\n'
            '    calls.write(json.dumps({"args": args, "cwd": os.getcwd()}) + "\\n")\n'
            'expected_node = os.environ.get("PNPM_TEST_EXPECT_NODE_VERSION")\n'
            'if expected_node:\n'
            '    assert subprocess.check_output(["node", "--version"], text=True).strip() == expected_node\n'
            'if args == ["--version"]:\n'
            '    print(os.environ.get("PNPM_TEST_VERSION", "10.6.5"))\n'
            'elif args == ["store", "path"]:\n'
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
        self.env['PNPM_TEST_EXPECT_NODE_VERSION'] = 'v24.20.0'
        self.git('add', '.')

    def git(self, *args):
        return subprocess.check_output(['git', *args], cwd=self.root)

    def check(self, env=None):
        return subprocess.run([sys.executable, 'scripts/pre_commit.py'], cwd=self.root,
                              env=env or self.env, capture_output=True, text=True, timeout=30)

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
        self.assertEqual(calls[0], {'args': ['--version'], 'cwd': calls[2]['cwd']})
        self.assertEqual(calls[1], {'args': ['store', 'path'], 'cwd': str((self.root / 'frontend').resolve())})
        self.assertEqual(calls[2]['args'], [
            'install', '--frozen-lockfile', '--offline', '--ignore-scripts',
            '--prod=false', '--store-dir', self.env['PNPM_TEST_STORE'],
        ])
        self.assertNotEqual(calls[2]['cwd'], str((self.root / 'frontend').resolve()))
        self.assertEqual(calls[3], {'args': ['run', 'check'], 'cwd': calls[2]['cwd']})

    def test_selects_installed_nvm_node_when_parent_environment_is_wrong(self):
        wrong_tools = Path(self.temp.name) / 'wrong-tools'
        wrong_tools.mkdir()
        wrong_node = wrong_tools / 'node'
        wrong_node.write_text('#!/bin/sh\nprintf "v20.19.0\\n"\n')
        wrong_node.chmod(0o755)
        nvm_root = Path(self.temp.name) / 'nvm'
        node_bin = nvm_root / 'versions/node/v24.20.0/bin'
        node_bin.mkdir(parents=True)
        shutil.copyfile(self.tools / 'node', node_bin / 'node')
        (node_bin / 'node').chmod(0o755)
        env = dict(self.env, NVM_DIR=str(nvm_root))
        env['PATH'] = str(wrong_tools) + os.pathsep + self.env['PATH']
        result = self.check(env)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('Using staged Node v24.20.0', result.stdout)

    def test_wrong_node_without_installed_nvm_version_fails_clearly(self):
        wrong_tools = Path(self.temp.name) / 'wrong-only'
        wrong_tools.mkdir()
        wrong_node = wrong_tools / 'node'
        wrong_node.write_text('#!/bin/sh\nprintf "v20.19.0\\n"\n')
        wrong_node.chmod(0o755)
        env = dict(self.env, NVM_DIR=str(Path(self.temp.name) / 'missing-nvm'))
        env['PATH'] = str(wrong_tools) + os.pathsep + os.defpath
        result = self.check(env)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('requires Node v24.20.0, but current Node is v20.19.0', result.stderr)
        self.assertIn('nvm install 24.20.0', result.stderr)
        self.assertFalse(self.calls.exists())

    def test_wrong_pnpm_version_fails_before_install(self):
        env = dict(self.env, PNPM_TEST_VERSION='9.0.0')
        result = self.check(env)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('requires pnpm 10.6.5', result.stderr)
        calls = [json.loads(line) for line in self.calls.read_text().splitlines()]
        self.assertEqual([call['args'] for call in calls], [['--version']])

    def test_failed_pnpm_install_blocks_checks_and_preserves_index(self):
        self.env['PNPM_TEST_FAIL_INSTALL'] = '1'
        index = self.git('ls-files', '--stage')
        result = self.check()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('pnpm install --frozen-lockfile', result.stderr)
        calls = [json.loads(line) for line in self.calls.read_text().splitlines()]
        self.assertEqual(len(calls), 3)
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

import importlib.util
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('sama_local', Path(__file__).with_name('local.py'))
local = importlib.util.module_from_spec(spec)
spec.loader.exec_module(local)


class LocalSetupTests(unittest.TestCase):
    def test_invalid_ports_origin_and_symlinks_fail_closed(self):
        with tempfile.TemporaryDirectory() as directory, patch.object(local, 'ROOT', Path(directory)), patch.dict(os.environ, {}, clear=True):
            for content in ['SAMA_DEV_DATABASE_PORT=8080', 'SAMA_DEV_OIDC_PORT=18543', 'SAMA_DEV_BACKEND_PORT=3000', 'SAMA_DEV_PUBLIC_ORIGIN=http://localhost:3000', 'SAMA_DEV_PUBLIC_ORIGIN=https://example.com', 'SAMA_DATABASE_URL=postgres://unexpected']:
                (Path(directory) / '.env').write_text(content)
                with self.assertRaises(ValueError):
                    local.settings()
            (Path(directory) / '.env').unlink()
            (Path(directory) / '.env').symlink_to('/dev/null')
            with self.assertRaises(ValueError):
                local.settings()

    def test_isolated_backend_origin_and_port(self):
        with tempfile.TemporaryDirectory() as directory, patch.object(local, 'ROOT', Path(directory)), patch.dict(os.environ, {
            'SAMA_DEV_BACKEND_PORT': '18580', 'SAMA_DEV_PUBLIC_ORIGIN': 'http://127.0.0.1:18580'
        }, clear=True):
            installation = object.__new__(local.Local)
            installation.config = local.settings()
            self.assertEqual(installation.environment()['SAMA_HTTP_ADDR'], '127.0.0.1:18580')
            self.assertEqual(installation.environment()['SAMA_PUBLIC_ORIGIN'], 'http://127.0.0.1:18580')

    def test_remote_context_rejected_before_docker_mutations(self):
        config = {**local.DEFAULTS, 'SAMA_DEV_DOCKER_CONTEXT': 'remote'}
        with patch.object(local, 'run', return_value=json.dumps([{'Endpoints': {'docker': {'Host': 'ssh://remote'}}}])) as run:
            with self.assertRaisesRegex(ValueError, 'local Unix-socket'):
                local.Local(config)
            self.assertEqual(run.call_args.args[0], ['docker', 'context', 'inspect', 'remote'])

    def test_ambient_production_settings_never_reach_backend(self):
        installation = object.__new__(local.Local)
        installation.config = local.DEFAULTS
        with patch.dict(os.environ, {'SAMA_DATABASE_URL': 'postgres://production', 'SAMA_OIDC_CLIENT_SECRET_FILE': '/production', 'SAMA_ENABLED_MUTATIONS': 'hetzner/cloud:delete', 'SAMA_WORKERS_ENABLED': '1'}):
            env = installation.environment()
        self.assertNotIn('SAMA_OIDC_CLIENT_SECRET_FILE', env)
        self.assertIn('sama_local_runtime@127.0.0.1:18543', env['SAMA_DATABASE_URL'])
        self.assertEqual(env['SAMA_WORKERS_ENABLED'], '0')
        self.assertEqual(env['SAMA_ENABLED_MUTATIONS'], '')
        self.assertEqual(env['SAMA_PROVIDER_CALLING_PROCESSES'], '0')

    def test_foreign_container_is_never_stopped(self):
        installation = object.__new__(local.Local)
        def call(*args, **kwargs):
            if args[:2] == ('container', 'ls'):
                return local.PREFIX + '-dex'
            if args[:2] == ('container', 'inspect'):
                return json.dumps([{'Config': {'Labels': {'owner': 'other-task'}}}])
            self.fail('Attempted mutation of foreign resource: ' + str(args))
        installation.call = call
        with self.assertRaisesRegex(ValueError, 'ownership label'):
            installation.down()

    def test_outside_invocation_keeps_paths_anchored(self):
        installation = object.__new__(local.Local)
        installation.config = local.DEFAULTS
        before = Path.cwd()
        with tempfile.TemporaryDirectory() as directory:
            try:
                os.chdir(directory)
                self.assertEqual(installation.environment()['SAMA_KEYRING_FILE'], str(local.STATE / 'keyring.json'))
                self.assertNotEqual(local.STATE.parent, Path.cwd())
            finally:
                os.chdir(before)

    def test_down_preserves_volume_and_state(self):
        installation = object.__new__(local.Local)
        calls = []
        installation.owned = lambda kind, name: True
        installation.call = lambda *args: calls.append(args)
        installation.down()
        self.assertEqual(calls, [
            ('stop', local.PREFIX + '-dex'), ('rm', local.PREFIX + '-dex'),
            ('stop', local.PREFIX + '-postgres'), ('rm', local.PREFIX + '-postgres'),
            ('network', 'rm', local.PREFIX),
        ])


if __name__ == '__main__':
    unittest.main()

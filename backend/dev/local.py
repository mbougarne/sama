#!/usr/bin/env python3
"""Opt-in, synthetic local installation; never imported by the production app."""
import argparse
import base64
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import time
import urllib.request

ROOT = Path(__file__).resolve().parent
BACKEND = ROOT.parent
STATE = ROOT / '.state'
LABEL = 'org.sama.local-development'
PREFIX = 'samadev-' + hashlib.sha256(str(ROOT).encode()).hexdigest()[:12]
# Official multi-architecture release indexes; update deliberately with the provider version.
POSTGRES = 'postgres:18-alpine@sha256:77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873'
DEX = 'ghcr.io/dexidp/dex:v2.46.0@sha256:933fcd3f523338c847b88ef84a7145fb2fcb7b9917f08418612afeaaa2001777'
# Dex v2.46.0 mockCallback identity + server/internal.IDTokenSubject protobuf.
USER = b'0-385-28089-0'
CONNECTOR = b'sama-local'
SUBJECT = base64.urlsafe_b64encode(b'\x0a' + bytes([len(USER)]) + USER + b'\x12' + bytes([len(CONNECTOR)]) + CONNECTOR).decode().rstrip('=')
DEFAULTS = {'SAMA_DEV_DOCKER_CONTEXT': '', 'SAMA_DEV_DATABASE_PORT': '18543',
            'SAMA_DEV_OIDC_PORT': '18556', 'SAMA_DEV_BACKEND_PORT': '8080',
            'SAMA_DEV_PUBLIC_ORIGIN': 'http://127.0.0.1:3000'}


def settings():
    values = DEFAULTS.copy()
    path = ROOT / '.env'
    if path.is_symlink():
        raise ValueError('Local configuration cannot be a symlink')
    if path.exists():
        for line in path.read_text().splitlines():
            if not line.strip() or line.lstrip().startswith('#'):
                continue
            key, value = line.split('=', 1)
            if key not in values:
                raise ValueError('Unknown local configuration field')
            values[key] = value.strip()
    values.update({key: os.environ[key] for key in values if key in os.environ})
    ports = [int(values[key]) for key in ['SAMA_DEV_DATABASE_PORT', 'SAMA_DEV_OIDC_PORT', 'SAMA_DEV_BACKEND_PORT']]
    if any(port < 1024 or port > 65535 or port == 3000 for port in ports) or len(set(ports)) != 3:
        raise ValueError('Use distinct unprivileged service ports, excluding frontend port 3000')
    if values['SAMA_DEV_PUBLIC_ORIGIN'] not in ['http://127.0.0.1:3000', 'http://127.0.0.1:' + values['SAMA_DEV_BACKEND_PORT']]:
        raise ValueError('Local public origin must be loopback frontend or configured backend port')
    return values


def run(args, data=None, env=None):
    try:
        return subprocess.run(args, input=data, text=True, capture_output=True,
                              check=True, timeout=300, cwd=BACKEND, env=env).stdout.strip()
    except subprocess.CalledProcessError as error:
        # Never print environment variables or arbitrary driver diagnostics.
        raise RuntimeError(Path(args[0]).name + ' failed with exit ' + str(error.returncode)) from None


class Local:
    def __init__(self, config):
        self.config = config
        context = config['SAMA_DEV_DOCKER_CONTEXT'] or run(['docker', 'context', 'show'])
        if not context or context.startswith('-'):
            raise ValueError('Select an existing local Docker context')
        self.context = context
        self.docker = ['docker', '--context', context]
        endpoint = json.loads(run(['docker', 'context', 'inspect', context]))[0]['Endpoints']['docker']['Host']
        if not endpoint.startswith('unix://'):
            raise ValueError('Local development requires a local Unix-socket Docker context')

    def call(self, *args, data=None):
        return run(self.docker + list(args), data=data)

    def owned(self, kind, name):
        args = [kind, 'ls'] + (['-a'] if kind == 'container' else [])
        names = self.call(*args, '--format', '{{.Names}}' if kind == 'container' else '{{.Name}}').splitlines()
        found = name in names
        if not found:
            return False
        info = json.loads(self.call(kind, 'inspect', name))[0]
        labels = info['Config'].get('Labels') if kind == 'container' else info.get('Labels')
        if (labels or {}).get(LABEL) != PREFIX:
            raise ValueError('Refusing to touch a resource without this checkout ownership label')
        return True

    def environment(self):
        # Do not inherit production database, identity, keyring or mutation settings.
        env = {key: value for key, value in os.environ.items() if not key.startswith('SAMA_')}
        env.update(SAMA_HTTP_ADDR='127.0.0.1:' + self.config['SAMA_DEV_BACKEND_PORT'], SAMA_PUBLIC_ORIGIN=self.config['SAMA_DEV_PUBLIC_ORIGIN'],
                   SAMA_DATABASE_URL='postgres://sama_local_runtime@127.0.0.1:' + self.config['SAMA_DEV_DATABASE_PORT'] + '/sama_local?sslmode=disable',
                   SAMA_MIGRATION_DATABASE_URL='postgres://sama_local_migrate@127.0.0.1:' + self.config['SAMA_DEV_DATABASE_PORT'] + '/sama_local?sslmode=disable',
                   SAMA_OIDC_ISSUER='http://127.0.0.1:' + self.config['SAMA_DEV_OIDC_PORT'] + '/dex',
                   SAMA_OIDC_CLIENT_ID='sama-local', SAMA_OIDC_DEVELOPMENT='true',
                   SAMA_KEYRING_FILE=str(STATE / 'keyring.json'), SAMA_WORKERS_ENABLED='0',
                   SAMA_ENABLED_MUTATIONS='', SAMA_PROVIDER_CALLING_PROCESSES='0', GOMAXPROCS='2')
        return env

    def sql(self, text):
        return self.call('exec', '-i', PREFIX + '-postgres', 'psql', '-v', 'ON_ERROR_STOP=1',
                         '-U', 'postgres', '-d', 'sama_local', '-At', data=text)

    def up(self):
        if STATE.is_symlink():
            raise ValueError('Local state cannot be a symlink')
        STATE.mkdir(exist_ok=True)
        env = self.environment()
        config = {'issuer': env['SAMA_OIDC_ISSUER'], 'storage': {'type': 'memory'},
                  'web': {'http': '0.0.0.0:5556'}, 'oauth2': {'responseTypes': ['code'],
                  'skipApprovalScreen': True, 'grantTypes': ['authorization_code'],
                  'pkce': {'enforce': True, 'codeChallengeMethodsSupported': ['S256']}},
                  'staticClients': [{'id': 'sama-local', 'public': True, 'name': 'Sama local development',
                  'redirectURIs': ['http://127.0.0.1:3000/auth/callback', 'http://127.0.0.1:' + self.config['SAMA_DEV_BACKEND_PORT'] + '/auth/callback']}],
                  'connectors': [{'type': 'mockCallback', 'id': CONNECTOR.decode(), 'name': 'Synthetic local user'}]}
        config_path = STATE / 'dex.json'
        keyring = STATE / 'keyring.json'
        for path in [config_path, keyring]:
            if path.is_symlink() or path.exists() and (not path.is_file() or path.stat().st_nlink != 1):
                raise ValueError('Local state files must be regular, single-link files')
        config_text = json.dumps(config)
        config_path.write_text(config_text)
        config_path.chmod(0o644)  # Public config must be readable by Dex's unprivileged UID.
        config_label = LABEL + '.config'
        config_hash = hashlib.sha256(config_text.encode()).hexdigest()
        if not keyring.exists():
            # Public test-only material, never a production encryption key.
            with open(keyring, 'x', opener=lambda path, flags: os.open(path, flags, 0o600)) as handle:
                json.dump({'active': 'local-only', 'keys': [{'id': 'local-only', 'key': base64.b64encode(bytes(32)).decode()}]}, handle)
        volume = PREFIX + '-data'
        if not self.owned('volume', volume):
            self.call('volume', 'create', '--label', LABEL + '=' + PREFIX, volume)
        if not self.owned('network', PREFIX):
            self.call('network', 'create', '--label', LABEL + '=' + PREFIX, PREFIX)
        if not self.owned('volume', volume) or not self.owned('network', PREFIX):
            raise ValueError('Local storage/network ownership could not be verified')
        specifications = [
            ('postgres', POSTGRES, self.config['SAMA_DEV_DATABASE_PORT'] + ':5432',
             ['-e', 'POSTGRES_HOST_AUTH_METHOD=trust', '-e', 'POSTGRES_DB=sama_local',
              '-v', volume + ':/var/lib/postgresql'],
             ['-c', 'max_connections=30', '-c', 'shared_buffers=32MB']),
            ('dex', DEX, self.config['SAMA_DEV_OIDC_PORT'] + ':5556',
             ['--tmpfs', '/tmp:rw,noexec,nosuid,size=16m'],
             ['dex', 'serve', '/etc/dex/local.json'])]
        started = []
        print('Starting synthetic checkout-local stack: ' + PREFIX, flush=True)
        try:
            for name, image, port, options, command in specifications:
                container = PREFIX + '-' + name
                if not self.owned('container', container):
                    started.append(container)
                    self.call('create', '--name', container, '--label', LABEL + '=' + PREFIX,
                              '--label', config_label + '=' + config_hash,
                              '--memory', '256m' if name == 'postgres' else '128m', '--cpus', '0.5',
                              '--pids-limit', '128', '--network', PREFIX, '-p', '127.0.0.1:' + port, *options, image, *command)
                    if name == 'dex':
                        self.call('cp', str(config_path), container + ':/etc/dex/local.json')
                else:
                    info = json.loads(self.call('container', 'inspect', container))[0]
                    binding = info['HostConfig']['PortBindings'].get(port.split(':')[1] + '/tcp')
                    if (info['Config']['Image'] != image or
                        info['Config']['Labels'].get(config_label) != config_hash or
                        binding != [{'HostIp': '127.0.0.1', 'HostPort': port.split(':')[0]}]):
                        raise ValueError('Existing local service config differs; run down before changing settings')
                self.call('start', container)
                if container not in started:
                    started.append(container)
            self.wait(lambda: self.call('exec', PREFIX + '-postgres', 'pg_isready', '-U', 'postgres'))
            self.wait(lambda: urllib.request.urlopen(env['SAMA_OIDC_ISSUER'] + '/.well-known/openid-configuration', timeout=2).close())
            self.sql("SELECT 'CREATE ROLE ' || role || ' LOGIN' FROM (VALUES ('sama_local_migrate'),('sama_local_runtime')) AS roles(role) WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname=role)\n\\gexec\nALTER SCHEMA public OWNER TO sama_local_migrate;\nGRANT CONNECT ON DATABASE sama_local TO sama_local_migrate;\n")
            print('Applying real database migrations and runtime grants', flush=True)
            run(['go', 'run', './cmd/sama-migrate'], env=env)
            self.call('exec', '-i', PREFIX + '-postgres', 'psql', '-v', 'ON_ERROR_STOP=1',
                      '-v', 'database_name=sama_local', '-v', 'migration_role=sama_local_migrate',
                      '-v', 'runtime_role=sama_local_runtime', '-U', 'postgres', '-d', 'sama_local',
                      data=(BACKEND / 'deploy/postgres/grants.sql').read_text())
            if self.sql('SELECT EXISTS(SELECT FROM installation_bootstrap);') == 'f':
                run(['go', 'run', './cmd/sama-bootstrap', '--issuer', env['SAMA_OIDC_ISSUER'],
                     '--subject', SUBJECT, '--workspace-name', 'Local Sama'], env=env)
            print('Local dependencies migrated and bootstrapped. Run: python3 backend/dev/local.py backend')
        except (Exception, KeyboardInterrupt):
            for container in started:
                if self.owned('container', container):
                    self.call('stop', container)
            raise

    @staticmethod
    def wait(probe):
        deadline = time.monotonic() + 90
        while True:
            try:
                probe()
                return
            except Exception:
                if time.monotonic() >= deadline:
                    raise RuntimeError('Local service did not become ready within 90 seconds') from None
                time.sleep(1)

    def status(self):
        print('Context: ' + self.context + '; checkout resources: ' + PREFIX)
        print(self.call('ps', '-a', '--filter', 'label=' + LABEL + '=' + PREFIX, '--format', '{{.Names}}: {{.Status}}'))

    def down(self):
        for suffix in ['dex', 'postgres']:
            name = PREFIX + '-' + suffix
            if self.owned('container', name):
                self.call('stop', name)
                self.call('rm', name)
        if self.owned('network', PREFIX):
            self.call('network', 'rm', PREFIX)
        print('Owned containers removed; local database volume and keyring preserved.')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('command', choices=['up', 'backend', 'down', 'status'])
    args = parser.parse_args()
    local = Local(settings())
    if args.command == 'backend':
        os.chdir(BACKEND)
        os.execvpe('go', ['go', 'run', './cmd/sama'], local.environment())
    getattr(local, args.command)()


if __name__ == '__main__':
    try:
        main()
    except (ValueError, OSError, subprocess.SubprocessError, RuntimeError) as error:
        print('Local setup failed: ' + (str(error) if isinstance(error, (ValueError, RuntimeError)) else 'check local Docker, Go, ports and service readiness; inspect owned container logs'), file=sys.stderr)
        sys.exit(1)

#!/usr/bin/env python3
"""Headless real PostgreSQL/OIDC acceptance; run after local.py up and a Go build."""
import http.cookiejar
import json
import os
import signal
import subprocess
import sys
import urllib.error
import urllib.request

import local


def check():
    installation = local.Local(local.settings())
    env = installation.environment()
    origin = env['SAMA_PUBLIC_ORIGIN']
    if origin != 'http://' + env['SAMA_HTTP_ADDR']:
        raise ValueError('Smoke check requires backend-only public origin')
    jar = http.cookiejar.CookieJar()
    browser = urllib.request.build_opener(urllib.request.ProxyHandler({}), urllib.request.HTTPCookieProcessor(jar))

    def request(path, data=None, headers=None):
        try:
            response = browser.open(urllib.request.Request(origin + path, data=data, headers=headers or {}), timeout=5)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            return response.status, response.read(65536)

    process = subprocess.Popen([str(local.BACKEND / 'bin/sama')], cwd=local.BACKEND,
                               env=env, start_new_session=True)
    try:
        def ready():
            if process.poll() is not None or request('/readyz')[0] != 200:
                raise RuntimeError('Backend not ready')
        installation.wait(ready)
        assert request('/api/v1/me')[0] == 401, 'Anonymous identity must fail'
        # The final redirect reaches / without SPA assets; /me proves the session.
        request('/auth/login')
        assert request('/api/v1/me')[0] == 200, 'Signed OIDC login failed'
        session = next(cookie for cookie in jar if cookie.name == 'sama_session')
        assert session.has_nonstandard_attr('HttpOnly'), 'Session must remain HttpOnly'
        status, payload = request('/api/v1/workspaces')
        workspaces = json.loads(payload)['data']
        assert status == 200 and len(workspaces) == 1 and workspaces[0]['role'] == 'owner', 'Bootstrap owner mismatch'
        assert installation.sql("SELECT NOT has_table_privilege('sama_local_runtime','audit_events','UPDATE') AND NOT has_table_privilege('sama_local_runtime','audit_events','DELETE') AND NOT has_table_privilege('sama_local_runtime','sama_schema_migrations','SELECT') AND NOT has_schema_privilege('sama_local_runtime','public','CREATE');") == 't', 'Runtime privileges too broad'
        installation.up()
        assert json.loads(request('/api/v1/workspaces')[1])['data'] == workspaces, 'Repeated setup changed owner/data'
        headers = {'Origin': origin, 'Content-Type': 'application/json'}
        assert request('/auth/logout', b'{}', headers)[0] == 403, 'Missing CSRF must fail'
        headers['X-CSRF-Token'] = next(cookie.value for cookie in jar if cookie.name == 'sama_csrf')
        assert request('/auth/logout', b'{}', headers)[0] == 204, 'Logout failed'
        assert request('/api/v1/me')[0] == 401, 'Logged-out identity must fail'
        print('PASS: real OIDC login, PostgreSQL owner/session, restricted grants, idempotent setup, CSRF and logout')
    finally:
        os.killpg(process.pid, signal.SIGTERM)
        try:
            process.wait(timeout=10)
        except subprocess.TimeoutExpired:
            os.killpg(process.pid, signal.SIGKILL)
            process.wait()


if __name__ == '__main__':
    try:
        check()
    except Exception:
        # Do not print authorization URLs, session cookies or driver diagnostics.
        print('Local authentication acceptance failed; inspect bounded service logs.', file=sys.stderr)
        sys.exit(1)

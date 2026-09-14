#!/usr/bin/env python3
"""Sama adapter for Agent Collaboration Standard 1.3.0; standard library only."""
from __future__ import annotations

import argparse
import contextlib
import datetime as dt
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import sys

sys.dont_write_bytecode = True
from agent_collaboration import upstream as engine

Error = engine.CollabError
FORMAT = 'sama-local-v2'
FOLDERS = {'conversation': 'conversations', 'input': 'inputs',
           'research': 'researches', 'commands': 'commands'}
MAX_FILE = 1024 * 1024
MAX_TOTAL = 100 * 1024 * 1024
MAX_FILES = 20000
MAX_HISTORY = 32 * 1024 * 1024
RESEARCH_SECTIONS = ('Epistemic Boundary', 'Observable Context', 'AI Concept',
                     'Worked Example', 'Connection to This Exchange',
                     'Limits', 'Sources', 'Learning Experiment')


def require(condition, message):
    if not condition:
        raise Error(message)


def timestamp():
    return dt.datetime.now(dt.timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ')


def parse_time(value):
    require(isinstance(value, str), 'timestamp must be a string')
    try:
        parsed = dt.datetime.strptime(value, '%Y-%m-%dT%H:%M:%SZ')
    except ValueError as exc:
        raise Error('timestamp must use UTC YYYY-MM-DDTHH:MM:SSZ') from exc
    return parsed


def stem(entry):
    return (parse_time(entry['recorded_at']).strftime('%Y-%m-%d-%H-%M-%S')
            + f"_{entry['event_id']}_{entry['sequence']:04d}.md")


def entry_digest(entry):
    value = {k: v for k, v in entry.items() if k != 'entry_sha256'}
    return hashlib.sha256(json.dumps(value, sort_keys=True, ensure_ascii=False,
                                    separators=(',', ':')).encode()).hexdigest()


def safe_path(repo, path):
    engine.assert_no_layout_symlinks(repo, [path])
    if path.exists():
        require(stat.S_ISREG(path.stat().st_mode) or path.is_dir(),
                f'not a regular file/directory: {path.name}')
        require(not path.is_file() or path.stat().st_nlink == 1,
                f'hard-linked files are not supported: {path.name}')


def read_text(repo, path, limit=MAX_FILE):
    safe_path(repo, path)
    require(path.is_file(), f'missing regular file: {path.relative_to(repo)}')
    require(path.stat().st_size <= limit, f'file exceeds {limit}-byte budget: {path.name}')
    with path.open('rb') as handle:
        raw = handle.read(limit + 1)
    require(len(raw) <= limit, f'file exceeds read budget: {path.name}')
    return raw.decode('utf-8')


def no_duplicate_keys(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, f'duplicate JSON key: {key}')
        result[key] = value
    return result


def load(repo):
    path = repo / 'agents/history.json'
    if not path.exists():
        safe_path(repo, path)
        return {'format': 'sama-local-v1', 'events': []}
    doc = json.loads(read_text(repo, path, MAX_HISTORY), object_pairs_hook=no_duplicate_keys)
    require(isinstance(doc, dict) and set(doc) == {'format', 'events'}, 'invalid history object')
    require(doc['format'] == 'sama-local-v1' and isinstance(doc['events'], list), 'invalid history format/events')
    require(len(doc['events']) <= engine.MAX_RECORDS, 'history event limit exceeded')
    return doc


def store(repo, doc):
    raw = (json.dumps(doc, indent=2, ensure_ascii=False) + '\n').encode()
    require(len(raw) <= MAX_HISTORY, 'history exceeds byte budget')
    engine.atomic_write(repo / 'agents/history.json', raw)


def privacy(text, label):
    findings = engine.scan_secret_text(text)
    require(not findings, f'possible credential in {label}: '
            + ', '.join(f'line {line} ({kind})' for line, kind in findings))


def scan(repo):
    base = repo / 'agents'
    safe_path(repo, base)
    found = set()
    total = count = 0
    if not base.exists():
        return found
    for parent, dirs, files in os.walk(base, followlinks=False):
        count += len(dirs) + len(files)
        require(count <= MAX_FILES, 'record store exceeds file/directory budget')
        for name in dirs:
            safe_path(repo, Path(parent) / name)
        for name in files:
            p = Path(parent) / name
            text = read_text(repo, p, MAX_HISTORY if p == base/'history.json' else MAX_FILE)
            total += p.stat().st_size
            require(total <= MAX_TOTAL, 'record store exceeds total byte budget')
            privacy(text, str(p.relative_to(base)))
            found.add(p.relative_to(base).as_posix())
    return found


def content_checks(kind, meta, body, event_id):
    require(not engine.PLACEHOLDER_RE.search(body), f'{kind} has unfinished template placeholders')
    require(body.strip(), f'empty {kind} body')
    if kind == 'conversation':
        problems = engine.validate_body(body)
        require(not problems, '; '.join(problems))
    elif kind == 'input':
        require(body.startswith('# User input\n\n') and body[len('# User input\n\n'):].strip(), 'empty input')
    elif kind == 'research':
        require(meta.get('value_kind') in engine.VALUE_KINDS, 'research value_kind must be observed, illustrative or mixed')
        require(isinstance(meta.get('concept'), str) and meta['concept'].strip(), 'research concept is required')
        require(len(body.splitlines()) <= 100, 'research body exceeds 100 lines')
        for title in RESEARCH_SECTIONS:
            require(engine.section(body, title), f'missing research section: {title}')
        boundary = engine.section(body, 'Epistemic Boundary')
        require(re.search(r'not model telemetry', boundary, re.I), 'research needs the not model telemetry boundary')
        require(re.search(r'illustrative|toy|observed', body, re.I), 'label research evidence/examples')
        worked = engine.section(body, 'Worked Example')
        require(len(worked.split()) >= 20, 'research needs a concrete worked example')
        require('No concept fit this exchange.' not in body, 'Sama requires an AI learning concept for every new input')
        require(not engine.RESEARCH_BOILERPLATE_RE.search(body), 'generic research boilerplate is not sufficient')
    elif kind == 'commands':
        # Use the shared engine command grammar with Sama companion metadata.
        if body.strip() == '# Command inventory\n\n' + engine.NO_COMMANDS:
            return
        matches = list(engine.COMMAND_HEADING_RE.finditer(body))
        require(matches and engine.NO_COMMANDS not in body, 'commands need headings or exact no-command statement')
        seen = set()
        for i, match in enumerate(matches):
            command = engine.normalize_command(match[1])
            require(command and command not in seen, 'duplicate/empty command entry')
            seen.add(command)
            part = body[match.end():matches[i+1].start() if i+1 < len(matches) else len(body)]
            require(engine.COMMAND_WHAT_RE.search(part) and engine.COMMAND_WHY_RE.search(part), 'command needs What and Why')


def validate(repo, doc, allow_drafts=False, complete=None):
    privacy(json.dumps(doc, ensure_ascii=False), 'history metadata')
    found = scan(repo)
    expected = {'history.json', '.history.lock'}
    seen = set()
    sequences = {}
    for entry in doc['events']:
        require(isinstance(entry, dict), 'event must be an object')
        eid = entry.get('event_id'); cid = entry.get('conversation_id')
        require(engine.uuid4_text(eid) and engine.uuid4_text(cid), 'event/conversation must be canonical UUIDv4')
        require(eid not in seen, 'duplicate event UUID')
        sup = entry.get('supersedes_event_id')
        require(sup is None or sup in seen, 'correction must reference an earlier event')
        seen.add(eid)
        seq = entry.get('sequence')
        require(type(seq) is int and seq == sequences.get(cid, 0) + 1, 'sequence gap/duplicate within conversation')
        sequences[cid] = seq
        require(entry.get('conversation_id_source') == 'local-allocation', 'conversation identity source required')
        for key in ('agent', 'topic'):
            require(isinstance(entry.get(key), str) and entry[key].strip(), f'{key} required')
        require(entry.get('role') in engine.ROLES, 'invalid agent role')
        require(engine.TOPIC_RE.fullmatch(entry['topic']), 'topic must be a lowercase kebab slug')
        name = stem(entry)
        state = entry.get('status')
        require(state in ('draft', 'final'), 'invalid event status')
        require(allow_drafts or state == 'final', f'unfinished draft: {eid}')
        if state == 'final':
            require(parse_time(entry.get('finalized_at')) >= parse_time(entry['recorded_at']), 'finalization precedes creation')
        else:
            require(entry.get('finalized_at') is None, 'draft must not have finalization time')
        version = entry.get('format', 'sama-local-v1')
        require(version in ('sama-local-v1', FORMAT), 'unknown event format')
        fields = {'event_id', 'conversation_id', 'conversation_id_source', 'sequence',
                  'recorded_at', 'topic', 'agent', 'role', 'status', 'finalized_at',
                  'supersedes_event_id', 'files', 'sha256', 'evidence'}
        if 'format' in entry:
            fields.add('format')
        if version == FORMAT and state == 'final':
            fields.add('entry_sha256')
        require(set(entry) == fields, 'unknown or missing event fields')
        paths = {k: f'{v}/{name}' for k,v in FOLDERS.items()}
        require(entry.get('files') == paths, 'four companion paths must exactly match the canonical basename')
        digests = entry.get('sha256')
        require(isinstance(digests, dict), 'sha256 must be an object')
        require(set(digests) == (set(FOLDERS) if state == 'final' else set()), 'invalid digest keys for event state')
        evidence = entry.get('evidence')
        require(isinstance(evidence, dict), 'evidence must be an object')
        if state == 'final':
            for key in ('scope', 'repo_head', 'repo_branch', 'verification'):
                require(isinstance(evidence.get(key), str) and evidence[key].strip(), f'evidence {key} required')
            require(evidence['scope'] in ('working-tree', 'staged', 'docs-only'), 'invalid evidence scope')
            require(evidence['repo_head'] == 'unborn' or engine.HEX_RE.fullmatch(evidence['repo_head']), 'invalid repository HEAD')
            evidence_fields = {'scope', 'repo_head', 'repo_branch', 'verification'}
            if version == FORMAT:
                evidence_fields.add('repo_state_sha256')
            require(set(evidence) == evidence_fields, 'unknown or missing evidence fields')
            if version == FORMAT:
                require(engine.SHA256_RE.fullmatch(evidence.get('repo_state_sha256', '')), 'repository fingerprint missing')
                require(entry.get('entry_sha256') == entry_digest(entry), 'final history metadata digest mismatch')
        for kind, relative in paths.items():
            expected.add(relative)
            text = read_text(repo, repo/'agents'/relative)
            meta, body = engine.parse_document(text)
            required = dict(format=version, event_id=eid, conversation_id=cid,
                            sequence=seq, recorded_at=entry['recorded_at'], kind=kind)
            require(all(meta.get(k) == v for k,v in required.items()), f'companion metadata mismatch: {relative}')
            if state == 'final':
                require(digests[kind] == hashlib.sha256(text.encode()).hexdigest(), f'final file digest mismatch: {relative}')
            if version == FORMAT and (state == 'final' or eid == complete):
                require(not engine.PLACEHOLDER_RE.search(json.dumps(meta)), f'{kind} metadata has unfinished placeholders')
                content_checks(kind, meta, body, eid)
    require(not (found - expected), 'orphan/unrecognized file: ' + ', '.join(sorted(found - expected)))
    return len(doc['events'])


@contextlib.contextmanager
def locked(repo, initialize=False):
    engine.validate_repo_root(repo)
    # Never write into an arbitrary folder or a Git subdirectory by accident.
    top = engine.run_git(repo, 'rev-parse', '--show-toplevel').decode().strip()
    require(Path(top).resolve() == repo, 'REPO must be the Git repository root')
    safe_path(repo, repo/'agents')
    if initialize:
        engine.run_git(repo, 'check-ignore', '-q', 'agents/history.json')
        require(not engine.run_git(repo, 'ls-files', '--', 'agents').strip(), 'agents/ contains tracked files')
        for folder in ['agents'] + [f'agents/{v}' for v in FOLDERS.values()]:
            p = repo/folder; safe_path(repo, p); p.mkdir(mode=0o700, parents=True, exist_ok=True)
    require((repo/'agents').is_dir(), 'store missing; run build first')
    safe_path(repo, repo/'agents/.history.lock')
    with engine.record_lock(repo/'agents/.history.lock'):
        yield


def template(kind):
    path = Path(__file__).parent/'agent_collaboration/templates'/f'{kind}.md'
    return path.read_text()


def new(repo, topic, agent, role, conversation, input_text, supersedes=None):
    require(engine.TOPIC_RE.fullmatch(topic), 'topic must be a lowercase kebab slug')
    require(agent.strip() and '\n' not in agent, 'agent identity must be nonempty and one line')
    require(role in engine.ROLES, 'invalid role')
    require(conversation is None or engine.uuid4_text(conversation), 'conversation must be UUIDv4')
    require(input_text.strip() and len(input_text.encode()) <= MAX_FILE - 4096, 'input is empty or exceeds budget')
    privacy(input_text, 'input'); privacy(agent, 'agent')
    with locked(repo, initialize=True):
        doc = load(repo); validate(repo, doc, allow_drafts=True)
        require(len(doc['events']) < engine.MAX_RECORDS, 'event limit exceeded')
        require(supersedes is None or any(e['event_id'] == supersedes and e['status'] == 'final' for e in doc['events']), 'correction target must be final')
        cid = conversation or str(engine.uuid.uuid4())
        eid = str(engine.uuid.uuid4())
        seq = 1 + max((e['sequence'] for e in doc['events'] if e['conversation_id'] == cid), default=0)
        entry = dict(format=FORMAT, event_id=eid, conversation_id=cid,
                     conversation_id_source='local-allocation', sequence=seq,
                     recorded_at=timestamp(), topic=topic, agent=agent, role=role,
                     status='draft', finalized_at=None, supersedes_event_id=supersedes,
                     files={}, sha256={}, evidence={})
        entry['files'] = {k: f'{v}/{stem(entry)}' for k,v in FOLDERS.items()}
        created = []
        try:
            for kind, relative in entry['files'].items():
                meta = {k:entry[k] for k in ('format','event_id','conversation_id','sequence','recorded_at')}
                meta['kind'] = kind
                if kind == 'research':
                    meta.update(concept='REPLACE_AI_CONCEPT', value_kind='mixed')
                body = '# User input\n\n' + input_text if kind == 'input' else template(kind)
                p = repo/'agents'/relative; safe_path(repo, p)
                engine.exclusive_write(p, engine.render_document(meta, body))
                created.append(p)
            doc['events'].append(entry); store(repo, doc)
        except Exception:
            # Roll back only files allocated by this operation; never remove older records.
            for p in created:
                p.unlink()
            raise
        return entry


def finalize(repo, event_id, scope):
    with locked(repo, initialize=True):
        doc = load(repo)
        entry = next((e for e in doc['events'] if e['event_id'] == event_id), None)
        require(entry is not None, 'unknown event UUID')
        require(entry['status'] == 'draft' and entry.get('format') == FORMAT, 'only v2 drafts can be finalized')
        validate(repo, doc, allow_drafts=True, complete=event_id)
        head, branch, digest = engine.repository_identity(repo, scope)
        _, body = engine.parse_document(read_text(repo, repo/'agents'/entry['files']['conversation']))
        entry['evidence'] = dict(scope=scope, repo_head=head, repo_branch=branch,
                                 repo_state_sha256=digest,
                                 verification=engine.summarize(engine.section(body, 'Verification'), 2000))
        entry['sha256'] = {k:hashlib.sha256((repo/'agents'/p).read_bytes()).hexdigest() for k,p in entry['files'].items()}
        entry['status'] = 'final'; entry['finalized_at'] = timestamp()
        entry['entry_sha256'] = entry_digest(entry)
        validate(repo, doc, allow_drafts=True)
        store(repo, doc)
        return entry


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest='command', required=True)
    for command in ('build', 'lint', 'new', 'finalize'):
        p = sub.add_parser(command); p.add_argument('repo', type=Path)
        if command == 'new':
            p.add_argument('topic'); p.add_argument('--agent', required=True)
            p.add_argument('--role', choices=sorted(engine.ROLES), required=True)
            p.add_argument('--conversation'); p.add_argument('--supersedes')
            p.add_argument('--input-file', type=Path, required=True)
        if command == 'finalize':
            p.add_argument('event_id'); p.add_argument('--evidence-scope', choices=['working-tree','staged','docs-only'], required=True)
    args = parser.parse_args(argv)
    repo = args.repo.resolve()
    try:
        if args.command == 'new':
            require(not args.input_file.is_symlink() and args.input_file.is_file(), 'input-file must be a regular nonsymlink file')
            require(args.input_file.stat().st_size <= MAX_FILE, 'input file exceeds budget')
            with args.input_file.open('rb') as handle:
                raw = handle.read(MAX_FILE+1)
            require(len(raw) <= MAX_FILE, 'input file exceeds budget')
            entry = new(repo, args.topic, args.agent, args.role, args.conversation,
                        raw.decode('utf-8'), args.supersedes)
            print(json.dumps(entry, indent=2))
        elif args.command == 'finalize':
            print('Finalized ' + finalize(repo, args.event_id, args.evidence_scope)['event_id'])
        elif args.command == 'build':
            with locked(repo, initialize=True):
                doc = load(repo); validate(repo, doc, allow_drafts=True)
                if not (repo/'agents/history.json').exists():
                    store(repo, doc)
            print('Store initialized; existing records preserved.')
        else:
            engine.validate_repo_root(repo)
            # A clean clone has no private records. Lint must not create them.
            if not (repo/'agents').exists() and not (repo/'agents').is_symlink():
                print('No local records; nothing to lint.')
                return 0
            with locked(repo):
                require((repo/'agents/history.json').exists(), 'history missing; run build only for an empty store')
                count = validate(repo, load(repo))
            print(f'PASS: {count} events; pairing, integrity, content and credential-pattern checks.')
        return 0
    except (Error, OSError, UnicodeError, ValueError, KeyError, TypeError) as exc:
        print(f'ERROR: {exc}', file=sys.stderr)
        return 1


if __name__ == '__main__':
    raise SystemExit(main())

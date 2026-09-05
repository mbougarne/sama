#!/usr/bin/env python3
"""Portable collaboration-ledger tooling.

Commands:
  collab.py new REPO TOPIC --agent NAME --role ROLE [--conversation UUID]
  collab.py finalize REPO RECORD --evidence-scope SCOPE
  collab.py build REPO
  collab.py lint REPO [--strict]

Only the forward layout under .agents/ledger and .agents/views is structurally
judged. Each event has a conversation record, research trace, and command
inventory. All regular files under .agents are still scanned for possible secrets.
"""

from __future__ import annotations

import argparse
import contextlib
import datetime as dt
import fcntl
import hashlib
import json
import os
import re
import subprocess
import sys
import tempfile
import threading
import time
import uuid
from pathlib import Path
from typing import Any, Iterable

SCHEMA_VERSION = 2
CONFIG_SCHEMA_VERSION = 1
KIT_ROOT = Path(__file__).resolve().parent.parent
RECORD_TEMPLATE = KIT_ROOT / "templates" / "conversation-record.md"
RESEARCH_TEMPLATE = KIT_ROOT / "templates" / "research-trace.md"
COMMAND_TEMPLATE = KIT_ROOT / "templates" / "command-inventory.md"
MAX_RECORDS = 10_000
MAX_RECORD_BYTES = 1 * 1024 * 1024
MAX_TEXT_FILE_BYTES = 5 * 1024 * 1024
MAX_SCAN_FILES = 20_000
MAX_SCAN_TOTAL_BYTES = 100 * 1024 * 1024
MAX_FINGERPRINT_BYTES = 256 * 1024 * 1024
MAX_GIT_METADATA_BYTES = 8 * 1024 * 1024
MAX_GENERATED_VIEW_BYTES = 32 * 1024 * 1024
MAX_SEAL_BYTES = 128
MAX_CONFIG_BYTES = 64 * 1024
STREAM_CHUNK_BYTES = 1024 * 1024
GIT_TIMEOUT_SECONDS = 60
LOCK_TIMEOUT_SECONDS = 30
ROLES = {"implementer", "reviewer", "analyst", "coordinator"}
VALUE_KINDS = {"observed", "illustrative", "mixed"}
FEATURE_KEYS = {"conversation_records", "research_traces", "command_inventories", "generated_handoff"}
PROFILE_DEFAULTS = {
    "full": {
        "conversation_records": True,
        "research_traces": True,
        "command_inventories": True,
        "generated_handoff": True,
    },
    "lean": {
        "conversation_records": True,
        "research_traces": False,
        "command_inventories": False,
        "generated_handoff": True,
    },
    "history-only": {
        "conversation_records": False,
        "research_traces": False,
        "command_inventories": False,
        "generated_handoff": False,
    },
}
TOPIC_RE = re.compile(r"^[a-z0-9]+(?:-[a-z0-9]+)*$")
AGENT_RE = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._-]*$")
RECORD_NAME_RE = re.compile(
    r"^(\d{4}-\d{2}-\d{2}-\d{2}-\d{2}-\d{2})_"
    r"([0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12})_"
    r"(\d{3})\.md$"
)
PLACEHOLDER_RE = re.compile(r"\bREPLACE_[A-Z0-9_]+\b")
COMMAND_HEADING_RE = re.compile(r"(?m)^## `([^`\n]+)`\s*$")
COMMAND_WHAT_RE = re.compile(r"(?m)^- \*\*What:\*\*\s*(\S.*)$")
COMMAND_WHY_RE = re.compile(r"(?m)^- \*\*Why:\*\*\s*(\S.*)$")
NO_COMMANDS = "No commands were run."
NO_CONCEPT = "No concept fit this exchange."
RESEARCH_BOILERPLATE_RE = re.compile(
    r"(?i)toy\s+tokens?\b[^\n]{0,140}?\b(?:pass\s+through\s+standard\s+attention"
    r"|be\s+represented\s+by\s+invented\s+vectors|can\s+be\s+represented\b)"
)
HEX_RE = re.compile(r"^[0-9a-f]{7,64}$")
SHA256_RE = re.compile(r"^[0-9a-f]{64}$")

FRONTMATTER_ORDER = [
    "schema_version",
    "event_id",
    "conversation_id",
    "sequence",
    "recorded_at",
    "finalized_at",
    "topic",
    "agent",
    "role",
    "status",
    "repo_head",
    "repo_branch",
    "repo_state_sha256",
    "evidence_scope",
    "supersedes_event_id",
    "project_state_changed",
    "research_trace_included",
    "command_inventory_included",
]

REQUIRED_SECTIONS = [
    "Request",
    "Interpretation",
    "Actions and changes",
    "Verification",
    "Decisions and rule candidates",
    "Uncertainty",
    "Handoff",
]
HANDOFF_SECTIONS = ["Done", "Next step", "Blockers", "Reading order"]

SECRET_PATTERNS = [
    ("private key", re.compile(r"-----BEGIN [A-Z ]*PRIVATE KEY-----")),
    ("AWS access key", re.compile(r"\bAKIA[0-9A-Z]{16}\b")),
    ("GitHub token", re.compile(r"\b(?:gh[pousr]_[A-Za-z0-9]{30,}|github_pat_[A-Za-z0-9_]{30,})\b")),
    ("OpenAI-style API key", re.compile(r"\bsk-[A-Za-z0-9_-]{20,}\b")),
    ("Slack token", re.compile(r"\bxox[baprs]-[A-Za-z0-9-]{10,}\b")),
    (
        "credential assignment",
        re.compile(
            r"(?i)\b(?:password|passwd|secret|api[_-]?key|access[_-]?token)\b"
            r"\s*[:=]\s*[\"']([^\"'<>]{8,})[\"']"
        ),
    ),
]
BENIGN_SECRET_VALUE = re.compile(
    r"(?i)^(?:enc\[|<|replace|placeholder|example|dummy|redacted|change[-_]?me|x{3,})"
)


class CollabError(Exception):
    """Expected user-facing validation failure."""


def load_config(repo: Path) -> dict[str, Any]:
    path = repo / ".agent-collab" / "config.json"
    profile = "full"
    overrides: dict[str, bool] = {}
    if path.is_symlink():
        raise CollabError(f"configuration file may not be a symlink: {path}")
    if path.exists():
        try:
            if path.stat().st_size > MAX_CONFIG_BYTES:
                raise CollabError(f"configuration exceeds the {MAX_CONFIG_BYTES}-byte limit")
            value = json.loads(path.read_text(encoding="utf-8"))
        except (OSError, UnicodeError, json.JSONDecodeError) as exc:
            raise CollabError(f"invalid collaboration configuration: {exc}") from exc
        if not isinstance(value, dict):
            raise CollabError("collaboration configuration must be a JSON object")
        unknown = sorted(set(value) - {"schema_version", "profile", "features"})
        if unknown:
            raise CollabError(f"unknown collaboration configuration fields: {', '.join(unknown)}")
        if value.get("schema_version") != CONFIG_SCHEMA_VERSION:
            raise CollabError(
                f"configuration schema_version must be {CONFIG_SCHEMA_VERSION}"
            )
        profile = value.get("profile", "full")
        if profile not in PROFILE_DEFAULTS:
            raise CollabError(f"profile must be one of {', '.join(PROFILE_DEFAULTS)}")
        raw_features = value.get("features", {})
        if not isinstance(raw_features, dict):
            raise CollabError("configuration features must be a JSON object")
        unknown_features = sorted(set(raw_features) - FEATURE_KEYS)
        if unknown_features:
            raise CollabError(f"unknown feature fields: {', '.join(unknown_features)}")
        for key, enabled in raw_features.items():
            if not isinstance(enabled, bool):
                raise CollabError(f"feature {key!r} must be true or false")
            overrides[key] = enabled
    features = dict(PROFILE_DEFAULTS[profile])
    features.update(overrides)
    if not features["conversation_records"] and any(
        features[key] for key in ("research_traces", "command_inventories", "generated_handoff")
    ):
        raise CollabError(
            "research_traces, command_inventories, and generated_handoff require conversation_records"
        )
    return {"profile": profile, "path": path, **features}


def now_rfc3339() -> str:
    return dt.datetime.now().astimezone().isoformat(timespec="seconds")


def uuid4_text(value: Any) -> bool:
    if not isinstance(value, str):
        return False
    try:
        parsed = uuid.UUID(value)
    except (ValueError, AttributeError):
        return False
    return parsed.version == 4 and str(parsed) == value


def parse_scalar(raw: str) -> Any:
    raw = raw.strip()
    if raw.startswith(('"', "'")):
        if raw.startswith('"'):
            try:
                return json.loads(raw)
            except json.JSONDecodeError as exc:
                raise CollabError(f"invalid quoted frontmatter value: {raw}") from exc
        if len(raw) >= 2 and raw.endswith("'"):
            return raw[1:-1]
    if raw == "true":
        return True
    if raw == "false":
        return False
    if re.fullmatch(r"-?\d+", raw):
        return int(raw)
    return raw


def parse_document(text: str) -> tuple[dict[str, Any], str]:
    lines = text.splitlines(keepends=True)
    if not lines or lines[0].strip() != "---":
        raise CollabError("missing YAML frontmatter")
    end = next((i for i, line in enumerate(lines[1:], 1) if line.strip() == "---"), None)
    if end is None:
        raise CollabError("unterminated YAML frontmatter")
    values: dict[str, Any] = {}
    for line_no, line in enumerate(lines[1:end], 2):
        stripped = line.strip()
        if not stripped or stripped.startswith("#"):
            continue
        if line[:1].isspace() or ":" not in line:
            raise CollabError(f"frontmatter must use flat key/value pairs (line {line_no})")
        key, raw = line.split(":", 1)
        key = key.strip()
        if not re.fullmatch(r"[a-z][a-z0-9_]*", key):
            raise CollabError(f"invalid frontmatter key {key!r} (line {line_no})")
        if key in values:
            raise CollabError(f"duplicate frontmatter key {key!r}")
        values[key] = parse_scalar(raw)
    return values, "".join(lines[end + 1 :])


def dump_scalar(value: Any) -> str:
    if isinstance(value, bool):
        return "true" if value else "false"
    if isinstance(value, int):
        return str(value)
    return json.dumps(str(value), ensure_ascii=False)


def render_document(frontmatter: dict[str, Any], body: str) -> str:
    unknown = sorted(set(frontmatter) - set(FRONTMATTER_ORDER))
    keys = [key for key in FRONTMATTER_ORDER if key in frontmatter] + unknown
    lines = ["---"] + [f"{key}: {dump_scalar(frontmatter[key])}" for key in keys] + ["---"]
    return "\n".join(lines) + "\n" + body.lstrip("\n")


def section(text: str, title: str, level: int = 2) -> str | None:
    hashes = "#" * level
    pattern = re.compile(rf"(?m)^{re.escape(hashes)} {re.escape(title)}\s*$")
    match = pattern.search(text)
    if not match:
        return None
    next_heading = re.compile(rf"(?m)^#{{1,{level}}} ").search(text, match.end())
    end = next_heading.start() if next_heading else len(text)
    return text[match.end() : end].strip()


def summarize(value: str | None, limit: int = 500) -> str:
    if not value:
        return ""
    compact = " ".join(line.strip() for line in value.splitlines() if line.strip())
    return compact[:limit]


def sha256_bytes(value: bytes) -> str:
    return hashlib.sha256(value).hexdigest()


def atomic_write(path: Path, data: bytes, mode: int = 0o600) -> None:
    if path.is_symlink() or path.parent.is_symlink():
        raise CollabError(f"refusing to write through a symlink: {path}")
    path.parent.mkdir(parents=True, exist_ok=True)
    fd, tmp_name = tempfile.mkstemp(prefix=f".{path.name}.", dir=path.parent)
    tmp = Path(tmp_name)
    try:
        with os.fdopen(fd, "wb") as handle:
            handle.write(data)
            handle.flush()
            os.fsync(handle.fileno())
        os.chmod(tmp, mode)
        os.replace(tmp, path)
    finally:
        with contextlib.suppress(FileNotFoundError):
            tmp.unlink()


def layout(repo: Path) -> dict[str, Path]:
    agents = repo / ".agents"
    ledger = agents / "ledger"
    return {
        "agents": agents,
        "ledger": ledger,
        "records": ledger / "records",
        "research": ledger / "research",
        "commands": ledger / "commands",
        "seals": ledger / "seals",
        "lock": ledger / ".record.lock",
        "views": agents / "views",
        "history": agents / "views" / "history.generated.jsonl",
        "handoff": agents / "views" / "NEXT_CONVERSATION.generated.md",
        "canonical_history": agents / "history.json",
        "history_lock": agents / ".history.lock",
    }


def validate_repo_root(repo: Path) -> None:
    if not repo.is_dir():
        raise CollabError(f"repository directory not found: {repo}")
    if repo == Path(repo.anchor):
        raise CollabError("refusing to use a filesystem root as a repository")


def assert_no_layout_symlinks(repo: Path, targets: Iterable[Path]) -> None:
    validate_repo_root(repo)
    resolved_repo = repo.resolve()
    for target in targets:
        try:
            relative = target.relative_to(repo)
        except ValueError as exc:
            raise CollabError(f"layout path escapes the repository: {target}") from exc
        current = repo
        for part in relative.parts:
            current = current / part
            if current.is_symlink():
                raise CollabError(f"layout path contains a symlink: {current}")
        if target.exists():
            try:
                target.resolve().relative_to(resolved_repo)
            except ValueError as exc:
                raise CollabError(f"layout path resolves outside the repository: {target}") from exc


def ensure_layout(repo: Path) -> dict[str, Path]:
    paths = layout(repo)
    targets = [
        paths["agents"],
        paths["ledger"],
        paths["records"],
        paths["research"],
        paths["commands"],
        paths["seals"],
        paths["views"],
    ]
    assert_no_layout_symlinks(repo, targets)
    for key in ("records", "research", "commands", "seals", "views"):
        paths[key].mkdir(parents=True, exist_ok=True)
    assert_no_layout_symlinks(repo, targets)
    return paths


@contextlib.contextmanager
def record_lock(path: Path) -> Iterable[None]:
    path.parent.mkdir(parents=True, exist_ok=True)
    if path.is_symlink() or path.parent.is_symlink():
        raise CollabError(f"refusing to lock through a symlink: {path}")
    with path.open("a+", encoding="utf-8") as handle:
        deadline = time.monotonic() + LOCK_TIMEOUT_SECONDS
        while True:
            try:
                fcntl.flock(handle.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
                break
            except BlockingIOError as exc:
                if time.monotonic() >= deadline:
                    raise CollabError(f"ledger lock exceeded the {LOCK_TIMEOUT_SECONDS}-second timeout") from exc
                time.sleep(0.05)
        try:
            yield
        finally:
            fcntl.flock(handle.fileno(), fcntl.LOCK_UN)


def run_git(repo: Path, *args: str, check: bool = True) -> bytes:
    try:
        result = subprocess.run(
            ["git", "-C", str(repo), *args],
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=False,
            timeout=GIT_TIMEOUT_SECONDS,
        )
    except subprocess.TimeoutExpired as exc:
        raise CollabError(f"git {' '.join(args)} exceeded the {GIT_TIMEOUT_SECONDS}-second timeout") from exc
    if check and result.returncode:
        detail = result.stderr.decode("utf-8", errors="replace").strip()
        raise CollabError(f"git {' '.join(args)} failed: {detail or 'unknown error'}")
    if len(result.stdout) > MAX_GIT_METADATA_BYTES:
        raise CollabError(f"git {' '.join(args)} exceeded the metadata output budget")
    return result.stdout


def update_digest_from_git(repo: Path, digest: Any, *args: str) -> int:
    total = 0
    with tempfile.TemporaryFile() as error_output:
        process = subprocess.Popen(
            ["git", "-C", str(repo), *args],
            stdout=subprocess.PIPE,
            stderr=error_output,
        )
        assert process.stdout is not None
        timed_out = threading.Event()

        def terminate_on_timeout() -> None:
            timed_out.set()
            with contextlib.suppress(ProcessLookupError):
                process.kill()

        timer = threading.Timer(GIT_TIMEOUT_SECONDS, terminate_on_timeout)
        timer.daemon = True
        timer.start()
        try:
            with process.stdout:
                while True:
                    chunk = process.stdout.read(STREAM_CHUNK_BYTES)
                    if not chunk:
                        break
                    total += len(chunk)
                    if total > MAX_FINGERPRINT_BYTES:
                        with contextlib.suppress(ProcessLookupError):
                            process.kill()
                        process.wait()
                        raise CollabError(
                            "repository diff exceeded the fingerprint budget; use a narrower staged or commit scope"
                        )
                    digest.update(chunk)
        finally:
            timer.cancel()
        return_code = process.wait()
        if timed_out.is_set():
            raise CollabError(f"git {' '.join(args)} exceeded the {GIT_TIMEOUT_SECONDS}-second timeout")
        if return_code:
            error_output.seek(0)
            detail = error_output.read(8192).decode("utf-8", errors="replace").strip()
            raise CollabError(f"git {' '.join(args)} failed: {detail or 'unknown error'}")
    return total


def update_digest_from_file(digest: Any, path: Path, consumed: int) -> int:
    if path.is_symlink():
        raise CollabError(f"refusing to fingerprint symlinked file: {path}")
    try:
        with path.open("rb") as handle:
            while True:
                chunk = handle.read(STREAM_CHUNK_BYTES)
                if not chunk:
                    break
                consumed += len(chunk)
                if consumed > MAX_FINGERPRINT_BYTES:
                    raise CollabError(
                        "untracked files exceeded the fingerprint budget; use a narrower staged or commit scope"
                    )
                digest.update(chunk)
    except OSError as exc:
        raise CollabError(f"could not fingerprint untracked path {path}: {exc}") from exc
    return consumed


def repository_identity(repo: Path, scope: str) -> tuple[str, str, str]:
    if scope == "not-applicable":
        return "not-applicable", "not-applicable", "not-applicable"
    inside = run_git(repo, "rev-parse", "--is-inside-work-tree", check=False).strip()
    if inside != b"true":
        raise CollabError(f"evidence scope {scope!r} requires a Git repository")

    head_raw = run_git(repo, "rev-parse", "--verify", "HEAD", check=False).strip()
    head = head_raw.decode() if head_raw else "unborn"
    branch_raw = run_git(repo, "symbolic-ref", "--quiet", "--short", "HEAD", check=False).strip()
    branch = branch_raw.decode("utf-8", errors="replace") if branch_raw else ("unborn" if head == "unborn" else "detached")

    digest = hashlib.sha256()
    digest.update(f"scope={scope}\0".encode())
    if scope.startswith("commit:"):
        ref = scope.split(":", 1)[1]
        if not ref:
            raise CollabError("commit evidence scope requires a ref")
        commit = run_git(repo, "rev-parse", "--verify", f"{ref}^{{commit}}").strip().decode()
        digest.update(commit.encode())
        return commit, f"ref:{ref}", digest.hexdigest()

    if scope not in {"working-tree", "staged", "docs-only"}:
        raise CollabError(f"unsupported evidence scope {scope!r}")
    digest.update(f"head={head}\0".encode())
    if scope == "staged":
        update_digest_from_git(
            repo,
            digest,
            "diff",
            "--cached",
            "--binary",
            "--no-ext-diff",
            "--",
            ".",
            ":(exclude)agents/**",
        )
        return head, branch, digest.hexdigest()

    consumed = 0
    if head == "unborn":
        consumed += update_digest_from_git(
            repo,
            digest,
            "diff",
            "--cached",
            "--binary",
            "--no-ext-diff",
            "--",
            ".",
            ":(exclude)agents/**",
        )
    else:
        consumed += update_digest_from_git(
            repo,
            digest,
            "diff",
            "--binary",
            "--no-ext-diff",
            "HEAD",
            "--",
            ".",
            ":(exclude)agents/**",
        )
    untracked = run_git(repo, "ls-files", "--others", "--exclude-standard", "-z").split(b"\0")
    for raw_name in sorted(name for name in untracked if name):
        rel = raw_name.decode("utf-8", errors="surrogateescape")
        if rel == "agents" or rel.startswith("agents/"):
            continue
        path = repo / rel
        digest.update(b"untracked\0" + raw_name + b"\0")
        try:
            if path.is_symlink():
                raise CollabError(f"refusing to fingerprint symlinked untracked path: {rel}")
            elif path.is_file():
                consumed = update_digest_from_file(digest, path, consumed)
        except OSError as exc:
            raise CollabError(f"could not fingerprint untracked path {rel}: {exc}") from exc
    return head, branch, digest.hexdigest()


def list_records(repo: Path) -> list[Path]:
    records = layout(repo)["records"]
    paths = sorted(records.glob("*.md")) if records.is_dir() else []
    if len(paths) > MAX_RECORDS:
        raise CollabError(f"record count exceeds the supported budget of {MAX_RECORDS}")
    for path in paths:
        if path.is_symlink():
            raise CollabError(f"record files may not be symlinks: {path}")
    return paths


def load_record(path: Path) -> tuple[dict[str, Any], str, bytes]:
    if path.is_symlink():
        raise CollabError(f"record files may not be symlinks: {path}")
    try:
        size = path.stat().st_size
    except OSError as exc:
        raise CollabError(f"could not inspect record: {exc}") from exc
    if size > MAX_RECORD_BYTES:
        raise CollabError(f"record exceeds the {MAX_RECORD_BYTES}-byte limit")
    raw = path.read_bytes()
    try:
        text = raw.decode("utf-8")
    except UnicodeDecodeError as exc:
        raise CollabError("record is not valid UTF-8") from exc
    frontmatter, body = parse_document(text)
    return frontmatter, body, raw


def load_companion(path: Path, label: str) -> tuple[dict[str, Any], str, bytes]:
    if path.is_symlink():
        raise CollabError(f"{label} files may not be symlinks: {path}")
    try:
        size = path.stat().st_size
    except FileNotFoundError as exc:
        raise CollabError(f"missing {label} file: {path.name}") from exc
    except OSError as exc:
        raise CollabError(f"could not inspect {label} file: {exc}") from exc
    if size > MAX_TEXT_FILE_BYTES:
        raise CollabError(f"{label} file exceeds the {MAX_TEXT_FILE_BYTES}-byte limit")
    raw = path.read_bytes()
    try:
        text = raw.decode("utf-8")
    except UnicodeDecodeError as exc:
        raise CollabError(f"{label} file is not valid UTF-8") from exc
    frontmatter, body = parse_document(text)
    return frontmatter, body, raw


def companion_metadata_problems(frontmatter: dict[str, Any], event_id: str, fields: set[str]) -> list[str]:
    problems: list[str] = []
    unknown = sorted(set(frontmatter) - fields)
    missing = sorted(fields - set(frontmatter))
    if unknown:
        problems.append(f"unknown frontmatter fields: {', '.join(unknown)}")
    if missing:
        problems.append(f"missing frontmatter fields: {', '.join(missing)}")
        return problems
    if frontmatter.get("schema_version") != SCHEMA_VERSION:
        problems.append(f"unsupported schema_version {frontmatter.get('schema_version')!r}")
    if frontmatter.get("record_event_id") != event_id:
        problems.append("record_event_id must match the paired conversation record")
    return problems


def validate_research_trace(path: Path, event_id: str) -> tuple[list[str], bytes]:
    try:
        frontmatter, body, raw = load_companion(path, "research")
    except CollabError as exc:
        return [str(exc)], b""
    problems = companion_metadata_problems(
        frontmatter,
        event_id,
        {"schema_version", "record_event_id", "concept", "value_kind"},
    )
    if len(raw.decode("utf-8").splitlines()) > 100:
        problems.append("research trace exceeds the 100-line limit")
    concept = frontmatter.get("concept")
    if not isinstance(concept, str) or not concept.strip():
        problems.append("concept must be a non-empty string")
    if frontmatter.get("value_kind") not in VALUE_KINDS:
        problems.append("value_kind must be observed, illustrative, or mixed")
    if PLACEHOLDER_RE.search(body):
        problems.append("research trace still contains template placeholders")
    if RESEARCH_BOILERPLATE_RE.search(body):
        problems.append("research trace contains the forbidden boilerplate attention example")
    worked = section(body, "The concept, worked")
    mapped = section(body, "What it meant here")
    if not worked:
        problems.append("missing or empty section: The concept, worked")
    if not mapped:
        problems.append("missing or empty section: What it meant here")
    no_concept = NO_CONCEPT in body
    if worked and not no_concept and len(re.findall(r"\b\w+\b", worked)) < 20:
        problems.append("worked concept is too thin; include concrete mechanics or a checkable example")
    if no_concept and worked and worked.strip() != NO_CONCEPT:
        problems.append(f"the no-concept alternative must be exactly {NO_CONCEPT!r}")
    if not re.search(r"not\s+model\s+telemetry", body, re.I):
        problems.append("missing model-telemetry disclaimer")
    return problems, raw


def normalize_command(command: str) -> str:
    return " ".join(command.split())


def validate_command_inventory(path: Path, event_id: str) -> tuple[list[str], bytes]:
    try:
        frontmatter, body, raw = load_companion(path, "command inventory")
    except CollabError as exc:
        return [str(exc)], b""
    problems = companion_metadata_problems(
        frontmatter,
        event_id,
        {"schema_version", "record_event_id"},
    )
    if PLACEHOLDER_RE.search(body):
        problems.append("command inventory still contains template placeholders")
    matches = list(COMMAND_HEADING_RE.finditer(body))
    if NO_COMMANDS in body:
        if body.strip() != f"# Command inventory\n\n{NO_COMMANDS}":
            problems.append(f"a no-command inventory must contain only the heading and {NO_COMMANDS!r}")
        if matches:
            problems.append("a no-command inventory cannot also contain command entries")
        return problems, raw
    if not matches:
        problems.append(f"list at least one command or state exactly {NO_COMMANDS!r}")
        return problems, raw
    seen: dict[str, str] = {}
    for index, match in enumerate(matches):
        command = match.group(1).strip()
        normalized = normalize_command(command)
        if not normalized:
            problems.append("command headings must not be empty")
            continue
        if normalized in seen:
            problems.append(f"duplicate command entry after whitespace normalization: {command}")
        else:
            seen[normalized] = command
        end = matches[index + 1].start() if index + 1 < len(matches) else len(body)
        entry = body[match.end() : end]
        if not COMMAND_WHAT_RE.search(entry):
            problems.append(f"command {command!r} is missing a brief What description")
        if not COMMAND_WHY_RE.search(entry):
            problems.append(f"command {command!r} is missing a brief Why reason")
    return problems, raw


def event_bundle_sha256(
    record_raw: bytes, research_raw: bytes | None = None, commands_raw: bytes | None = None
) -> str:
    digest = hashlib.sha256()
    parts = [(b"record", record_raw)]
    if research_raw is not None:
        parts.append((b"research", research_raw))
    if commands_raw is not None:
        parts.append((b"commands", commands_raw))
    for label, raw in parts:
        digest.update(label + b"\0" + str(len(raw)).encode("ascii") + b"\0" + raw)
    return digest.hexdigest()


def read_seal(path: Path) -> str:
    if path.is_symlink():
        raise CollabError(f"seal files may not be symlinks: {path}")
    try:
        if path.stat().st_size > MAX_SEAL_BYTES:
            raise CollabError(f"seal exceeds the {MAX_SEAL_BYTES}-byte limit: {path}")
        return path.read_text(encoding="ascii").strip()
    except FileNotFoundError:
        return ""
    except (OSError, UnicodeError) as exc:
        raise CollabError(f"could not read seal {path}: {exc}") from exc


def validate_body(body: str) -> list[str]:
    problems: list[str] = []
    if PLACEHOLDER_RE.search(body):
        problems.append("record still contains template placeholders")
    values = {name: section(body, name) for name in REQUIRED_SECTIONS}
    for name, value in values.items():
        if not value:
            problems.append(f"missing or empty section: {name}")
    request = values.get("Request") or ""
    if not any(line.startswith("> ") and line[2:].strip() for line in request.splitlines()):
        problems.append("Request must contain a sanitized verbatim quote")
    verification = values.get("Verification") or ""
    for name in ("Verified", "Not run"):
        if not section(verification, name, level=3):
            problems.append(f"Verification must contain a non-empty {name!r} subsection")
    handoff = values.get("Handoff") or ""
    for name in HANDOFF_SECTIONS:
        if not section(handoff, name, level=3):
            problems.append(f"Handoff must contain a non-empty {name!r} subsection")
    return problems


def metadata_problems(frontmatter: dict[str, Any], path: Path | None = None, final: bool = True) -> list[str]:
    problems: list[str] = []
    missing = [key for key in FRONTMATTER_ORDER if key not in frontmatter]
    unknown = sorted(set(frontmatter) - set(FRONTMATTER_ORDER))
    if unknown:
        problems.append(f"unknown frontmatter fields: {', '.join(unknown)}")
    if missing:
        problems.append(f"missing frontmatter fields: {', '.join(missing)}")
        return problems
    if frontmatter["schema_version"] != SCHEMA_VERSION:
        problems.append(f"unsupported schema_version {frontmatter['schema_version']!r}")
    for key in ("event_id", "conversation_id"):
        if not uuid4_text(frontmatter[key]):
            problems.append(f"{key} must be a canonical lowercase UUIDv4")
    if not isinstance(frontmatter["sequence"], int) or frontmatter["sequence"] < 1:
        problems.append("sequence must be a positive integer")
    if not isinstance(frontmatter["topic"], str) or not TOPIC_RE.fullmatch(frontmatter["topic"]):
        problems.append("topic must be a kebab-case slug")
    if frontmatter["role"] not in ROLES:
        problems.append(f"role must be one of {', '.join(sorted(ROLES))}")
    if not isinstance(frontmatter["agent"], str) or not AGENT_RE.fullmatch(frontmatter["agent"]):
        problems.append("agent has an invalid name")
    if not isinstance(frontmatter["project_state_changed"], bool):
        problems.append("project_state_changed must be true or false")
    for key in ("research_trace_included", "command_inventory_included"):
        if not isinstance(frontmatter[key], bool):
            problems.append(f"{key} must be true or false")
    for key in ("recorded_at", "finalized_at"):
        value = frontmatter[key]
        if key == "finalized_at" and not final and value == "pending":
            continue
        try:
            parsed = dt.datetime.fromisoformat(str(value))
            if parsed.tzinfo is None:
                raise ValueError("timezone missing")
        except ValueError:
            problems.append(f"{key} must be an RFC3339 timestamp with timezone")
    expected_status = "final" if final else "draft"
    if frontmatter["status"] != expected_status:
        problems.append(f"status must be {expected_status!r}")
    if final:
        scope = str(frontmatter["evidence_scope"])
        valid_scope = scope in {"working-tree", "staged", "docs-only", "not-applicable"} or (
            scope.startswith("commit:") and len(scope) > len("commit:")
        )
        if not valid_scope:
            problems.append("invalid evidence_scope")
        if scope == "not-applicable":
            if any(frontmatter[key] != "not-applicable" for key in ("repo_head", "repo_branch", "repo_state_sha256")):
                problems.append("not-applicable evidence requires not-applicable repository identity")
        else:
            if frontmatter["repo_head"] != "unborn" and not HEX_RE.fullmatch(str(frontmatter["repo_head"])):
                problems.append("repo_head must be a commit hash or unborn")
            if not isinstance(frontmatter["repo_branch"], str) or not frontmatter["repo_branch"]:
                problems.append("repo_branch must be non-empty")
            if not SHA256_RE.fullmatch(str(frontmatter["repo_state_sha256"])):
                problems.append("repo_state_sha256 must be a SHA-256 digest")
    supersedes = frontmatter["supersedes_event_id"]
    if supersedes != "none" and not uuid4_text(supersedes):
        problems.append("supersedes_event_id must be a UUIDv4 or none")
    if supersedes == frontmatter.get("event_id"):
        problems.append("a record cannot supersede itself")
    if path is not None:
        match = RECORD_NAME_RE.fullmatch(path.name)
        if not match:
            problems.append("filename does not match timestamp_event-id_seq.md")
        else:
            if match.group(2) != frontmatter.get("event_id"):
                problems.append("filename event ID differs from frontmatter")
            if int(match.group(3)) != frontmatter.get("sequence"):
                problems.append("filename sequence differs from frontmatter")
            if str(frontmatter.get("recorded_at", ""))[:19].replace("T", "-").replace(":", "-") != match.group(1):
                problems.append("filename timestamp differs from recorded_at")
    return problems


def scan_secret_text(text: str) -> list[tuple[int, str]]:
    findings: list[tuple[int, str]] = []
    for line_no, line in enumerate(text.splitlines(), 1):
        for label, pattern in SECRET_PATTERNS:
            match = pattern.search(line)
            if not match:
                continue
            captured = match.group(match.lastindex) if match.lastindex else ""
            if captured and BENIGN_SECRET_VALUE.match(captured):
                continue
            findings.append((line_no, label))
            break
    return findings


def exclusive_write(path: Path, text: str) -> None:
    try:
        fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    except FileExistsError as exc:
        raise CollabError(f"refusing to overwrite existing file: {path}") from exc
    with os.fdopen(fd, "w", encoding="utf-8") as handle:
        handle.write(text)
        handle.flush()
        os.fsync(handle.fileno())


HISTORY_ENTRY_FIELDS = {
    "event_id",
    "conversation_id",
    "sequence",
    "recorded_at",
    "finalized_at",
    "topic",
    "agent",
    "role",
    "status",
    "request",
    "verification",
    "repo_head",
    "repo_branch",
    "repo_state_sha256",
    "evidence_scope",
    "supersedes_event_id",
    "project_state_changed",
    "entry_sha256",
}


def history_document_bytes(entries: list[dict[str, Any]]) -> bytes:
    value = {"schema_version": SCHEMA_VERSION, "mode": "history-only", "entries": entries}
    raw = (json.dumps(value, ensure_ascii=False, indent=2) + "\n").encode("utf-8")
    if len(raw) > MAX_GENERATED_VIEW_BYTES:
        raise CollabError(f"canonical history exceeds the {MAX_GENERATED_VIEW_BYTES}-byte budget")
    return raw


def load_history_document(path: Path) -> list[dict[str, Any]]:
    if path.is_symlink():
        raise CollabError(f"canonical history may not be a symlink: {path}")
    try:
        if path.stat().st_size > MAX_GENERATED_VIEW_BYTES:
            raise CollabError(f"canonical history exceeds the {MAX_GENERATED_VIEW_BYTES}-byte budget")
        value = json.loads(path.read_text(encoding="utf-8"))
    except FileNotFoundError as exc:
        raise CollabError("canonical .agents/history.json is missing; run build") from exc
    except (OSError, UnicodeError, json.JSONDecodeError) as exc:
        raise CollabError(f"invalid canonical history: {exc}") from exc
    if not isinstance(value, dict):
        raise CollabError("canonical history root must be an object")
    if set(value) != {"schema_version", "mode", "entries"}:
        raise CollabError("canonical history must contain only schema_version, mode, and entries")
    if value.get("schema_version") != SCHEMA_VERSION or value.get("mode") != "history-only":
        raise CollabError("canonical history has an unsupported schema_version or mode")
    entries = value.get("entries")
    if not isinstance(entries, list):
        raise CollabError("canonical history entries must be an array")
    if len(entries) > MAX_RECORDS:
        raise CollabError(f"canonical history contains more than {MAX_RECORDS} entries")
    if not all(isinstance(entry, dict) for entry in entries):
        raise CollabError("every canonical history entry must be an object")
    return entries


def ensure_history_store(repo: Path) -> tuple[dict[str, Path], list[dict[str, Any]]]:
    validate_repo_root(repo)
    paths = layout(repo)
    assert_no_layout_symlinks(
        repo, [paths["agents"], paths["canonical_history"], paths["history_lock"]]
    )
    paths["agents"].mkdir(parents=True, exist_ok=True)
    if not paths["canonical_history"].exists():
        atomic_write(paths["canonical_history"], history_document_bytes([]))
    return paths, load_history_document(paths["canonical_history"])


def entry_sha256(entry: dict[str, Any]) -> str:
    value = {key: entry[key] for key in sorted(entry) if key != "entry_sha256"}
    raw = json.dumps(value, ensure_ascii=False, separators=(",", ":"), sort_keys=True).encode("utf-8")
    return sha256_bytes(raw)


def history_entry_problems(entry: dict[str, Any], final: bool) -> list[str]:
    problems: list[str] = []
    unknown = sorted(set(entry) - HISTORY_ENTRY_FIELDS)
    missing = sorted(HISTORY_ENTRY_FIELDS - set(entry))
    if unknown:
        problems.append(f"unknown fields: {', '.join(unknown)}")
    if missing:
        problems.append(f"missing fields: {', '.join(missing)}")
        return problems
    for key in ("event_id", "conversation_id"):
        if not uuid4_text(entry[key]):
            problems.append(f"{key} must be a canonical lowercase UUIDv4")
    if not isinstance(entry["sequence"], int) or entry["sequence"] < 1:
        problems.append("sequence must be a positive integer")
    if not isinstance(entry["topic"], str) or not TOPIC_RE.fullmatch(entry["topic"]):
        problems.append("topic must be a kebab-case slug")
    if entry["role"] not in ROLES:
        problems.append(f"role must be one of {', '.join(sorted(ROLES))}")
    if not isinstance(entry["agent"], str) or not AGENT_RE.fullmatch(entry["agent"]):
        problems.append("agent has an invalid name")
    if not isinstance(entry["project_state_changed"], bool):
        problems.append("project_state_changed must be true or false")
    for key in ("request", "verification"):
        if not isinstance(entry[key], str) or not entry[key].strip():
            problems.append(f"{key} must be a non-empty string")
        elif final and PLACEHOLDER_RE.search(entry[key]):
            problems.append(f"{key} still contains a template placeholder")
    try:
        recorded = dt.datetime.fromisoformat(str(entry["recorded_at"]))
        if recorded.tzinfo is None:
            raise ValueError
    except ValueError:
        problems.append("recorded_at must be an RFC3339 timestamp with timezone")
    if final:
        try:
            finalized = dt.datetime.fromisoformat(str(entry["finalized_at"]))
            if finalized.tzinfo is None:
                raise ValueError
        except ValueError:
            problems.append("finalized_at must be an RFC3339 timestamp with timezone")
        scope = str(entry["evidence_scope"])
        valid_scope = scope in {"working-tree", "staged", "docs-only", "not-applicable"} or (
            scope.startswith("commit:") and len(scope) > len("commit:")
        )
        if not valid_scope:
            problems.append("invalid evidence_scope")
        if scope == "not-applicable":
            if any(entry[key] != "not-applicable" for key in ("repo_head", "repo_branch", "repo_state_sha256")):
                problems.append("not-applicable evidence requires not-applicable repository identity")
        else:
            if entry["repo_head"] != "unborn" and not HEX_RE.fullmatch(str(entry["repo_head"])):
                problems.append("repo_head must be a commit hash or unborn")
            if not isinstance(entry["repo_branch"], str) or not entry["repo_branch"]:
                problems.append("repo_branch must be non-empty")
            if not SHA256_RE.fullmatch(str(entry["repo_state_sha256"])):
                problems.append("repo_state_sha256 must be a SHA-256 digest")
        if entry["status"] != "final":
            problems.append("status must be 'final'")
        if not SHA256_RE.fullmatch(str(entry["entry_sha256"])):
            problems.append("entry_sha256 must be a SHA-256 digest")
    else:
        if entry["status"] != "draft":
            problems.append("status must be 'draft'")
        if entry["finalized_at"] != "pending" or entry["entry_sha256"] != "pending":
            problems.append("draft finalized_at and entry_sha256 must be pending")
    supersedes = entry["supersedes_event_id"]
    if supersedes != "none" and not uuid4_text(supersedes):
        problems.append("supersedes_event_id must be a UUIDv4 or none")
    if supersedes == entry.get("event_id"):
        problems.append("an entry cannot supersede itself")
    return problems


def create_history_event(
    repo: Path, topic: str, agent: str, role: str, conversation: str | None
) -> Path:
    paths = layout(repo)
    paths["agents"].mkdir(parents=True, exist_ok=True)
    with record_lock(paths["history_lock"]):
        paths, entries = ensure_history_store(repo)
        conversation_id = conversation or str(uuid.uuid4())
        sequences = [
            entry.get("sequence")
            for entry in entries
            if entry.get("conversation_id") == conversation_id and isinstance(entry.get("sequence"), int)
        ]
        sequence = max(sequences, default=0) + 1
        if sequence > 999:
            raise CollabError("conversation exceeded the three-digit sequence limit; start a new conversation")
        event_id = str(uuid.uuid4())
        entry = {
            "event_id": event_id,
            "conversation_id": conversation_id,
            "sequence": sequence,
            "recorded_at": now_rfc3339(),
            "finalized_at": "pending",
            "topic": topic,
            "agent": agent,
            "role": role,
            "status": "draft",
            "request": "REPLACE_REQUEST_SUMMARY",
            "verification": "REPLACE_VERIFICATION_SUMMARY",
            "repo_head": "pending",
            "repo_branch": "pending",
            "repo_state_sha256": "pending",
            "evidence_scope": "pending",
            "supersedes_event_id": "none",
            "project_state_changed": False,
            "entry_sha256": "pending",
        }
        entries.append(entry)
        atomic_write(paths["canonical_history"], history_document_bytes(entries))
    print(f"history: {paths['canonical_history']}")
    print(f"event_id: {event_id}")
    print(f"conversation_id: {conversation_id}")
    print(f"sequence: {sequence}")
    return paths["canonical_history"]


def create_record(repo: Path, topic: str, agent: str, role: str, conversation: str | None) -> Path:
    validate_repo_root(repo)
    config = load_config(repo)
    if not TOPIC_RE.fullmatch(topic):
        raise CollabError("topic must be a lowercase kebab-case slug")
    if not AGENT_RE.fullmatch(agent):
        raise CollabError("agent must contain only letters, digits, dot, underscore, or hyphen")
    if role not in ROLES:
        raise CollabError(f"role must be one of {', '.join(sorted(ROLES))}")
    if conversation is not None and not uuid4_text(conversation):
        raise CollabError("--conversation must be a canonical lowercase UUIDv4")
    if not config["conversation_records"]:
        return create_history_event(repo, topic, agent, role, conversation)
    conversation_id = conversation or str(uuid.uuid4())
    paths = ensure_layout(repo)
    for template in (RECORD_TEMPLATE, RESEARCH_TEMPLATE, COMMAND_TEMPLATE):
        if not template.is_file():
            raise CollabError(f"required template not found: {template}")

    with record_lock(paths["lock"]):
        used_sequences: list[int] = []
        for existing in list_records(repo):
            try:
                frontmatter, _, _ = load_record(existing)
            except (OSError, CollabError) as exc:
                raise CollabError(f"cannot allocate a sequence while {existing.name} is invalid: {exc}") from exc
            if frontmatter.get("conversation_id") == conversation_id:
                seq = frontmatter.get("sequence")
                if isinstance(seq, int) and seq > 0:
                    used_sequences.append(seq)
        sequence = max(used_sequences, default=0) + 1
        if sequence > 999:
            raise CollabError("conversation exceeded the three-digit sequence limit; start a new conversation")
        event_id = str(uuid.uuid4())
        recorded_at = now_rfc3339()
        stamp = recorded_at[:19].replace("T", "-").replace(":", "-")
        destination = paths["records"] / f"{stamp}_{event_id}_{sequence:03d}.md"
        text = RECORD_TEMPLATE.read_text(encoding="utf-8")
        replacements = {
            "REPLACE_EVENT_ID": event_id,
            "REPLACE_CONVERSATION_ID": conversation_id,
            "REPLACE_SEQUENCE": str(sequence),
            "REPLACE_RECORDED_AT": recorded_at,
            "REPLACE_TOPIC": topic,
            "REPLACE_AGENT": agent,
            "REPLACE_ROLE": role,
            "REPLACE_TITLE": topic.replace("-", " ").title(),
            "REPLACE_RESEARCH_TRACE_INCLUDED": str(config["research_traces"]).lower(),
            "REPLACE_COMMAND_INVENTORY_INCLUDED": str(config["command_inventories"]).lower(),
        }
        for old, new in replacements.items():
            text = text.replace(old, new)
        research_destination = paths["research"] / f"{event_id}.md"
        commands_destination = paths["commands"] / f"{event_id}.md"
        research_text = RESEARCH_TEMPLATE.read_text(encoding="utf-8").replace(
            "REPLACE_RECORD_EVENT_ID", event_id
        )
        commands_text = COMMAND_TEMPLATE.read_text(encoding="utf-8").replace(
            "REPLACE_RECORD_EVENT_ID", event_id
        )
        created: list[Path] = []
        try:
            targets = [(destination, text)]
            if config["research_traces"]:
                targets.append((research_destination, research_text))
            if config["command_inventories"]:
                targets.append((commands_destination, commands_text))
            for target, content in targets:
                exclusive_write(target, content)
                created.append(target)
        except (OSError, CollabError):
            for target in created:
                with contextlib.suppress(OSError):
                    target.unlink()
            raise
    print(f"record: {destination}")
    if config["research_traces"]:
        print(f"research: {research_destination}")
    if config["command_inventories"]:
        print(f"commands: {commands_destination}")
    print(f"event_id: {event_id}")
    print(f"conversation_id: {conversation_id}")
    print(f"sequence: {sequence}")
    return destination


def resolve_record(repo: Path, value: str) -> Path:
    supplied = Path(value)
    candidates = [supplied] if supplied.is_absolute() else [repo / supplied, layout(repo)["records"] / supplied]
    path = next((candidate.resolve() for candidate in candidates if candidate.is_file()), None)
    if path is None:
        raise CollabError(f"record not found: {value}")
    records_root = layout(repo)["records"].resolve()
    try:
        path.relative_to(records_root)
    except ValueError as exc:
        raise CollabError("record must be under .agents/ledger/records") from exc
    return path


def record_info(path: Path, frontmatter: dict[str, Any], body: str, raw: bytes) -> dict[str, Any]:
    return {"path": path, "frontmatter": frontmatter, "body": body, "raw": raw, "sha256": sha256_bytes(raw)}


def validate_final_record(info: dict[str, Any]) -> list[str]:
    return metadata_problems(info["frontmatter"], info["path"], final=True) + validate_body(info["body"])


def final_record_infos(repo: Path, require_integrity: bool = True) -> list[dict[str, Any]]:
    infos: list[dict[str, Any]] = []
    paths = layout(repo)
    seen_events: set[str] = set()
    seen_pairs: set[tuple[str, int]] = set()
    for path in list_records(repo):
        frontmatter, body, raw = load_record(path)
        if frontmatter.get("status") != "final":
            continue
        info = record_info(path, frontmatter, body, raw)
        problems = validate_final_record(info)
        if problems:
            raise CollabError(f"cannot build from invalid record {path.name}: {'; '.join(problems)}")
        event_id = frontmatter["event_id"]
        pair = (frontmatter["conversation_id"], frontmatter["sequence"])
        if event_id in seen_events:
            raise CollabError(f"duplicate event_id {event_id}")
        if pair in seen_pairs:
            raise CollabError(f"duplicate conversation sequence {pair[1]} for {pair[0]}")
        seen_events.add(event_id)
        seen_pairs.add(pair)
        research_raw = None
        commands_raw = None
        if frontmatter["research_trace_included"]:
            research_problems, research_raw = validate_research_trace(
                paths["research"] / f"{event_id}.md", event_id
            )
            if research_problems:
                raise CollabError(
                    f"cannot build from invalid research {event_id}.md: {'; '.join(research_problems)}"
                )
        if frontmatter["command_inventory_included"]:
            command_problems, commands_raw = validate_command_inventory(
                paths["commands"] / f"{event_id}.md", event_id
            )
            if command_problems:
                raise CollabError(
                    f"cannot build from invalid command inventory {event_id}.md: {'; '.join(command_problems)}"
                )
        if require_integrity:
            seal = paths["seals"] / f"{event_id}.sha256"
            expected = event_bundle_sha256(raw, research_raw, commands_raw)
            actual = read_seal(seal)
            if actual != expected:
                raise CollabError(f"missing or broken seal for {path.name}")
        infos.append(info)
    event_ids = {info["frontmatter"]["event_id"] for info in infos}
    for info in infos:
        target = info["frontmatter"]["supersedes_event_id"]
        if target != "none" and target not in event_ids:
            raise CollabError(f"{info['path'].name} supersedes unknown event {target}")
    return infos


def history_row(repo: Path, info: dict[str, Any]) -> dict[str, Any]:
    fm = info["frontmatter"]
    return {
        "schema_version": SCHEMA_VERSION,
        "event_id": fm["event_id"],
        "conversation_id": fm["conversation_id"],
        "sequence": fm["sequence"],
        "recorded_at": fm["recorded_at"],
        "finalized_at": fm["finalized_at"],
        "topic": fm["topic"],
        "agent": fm["agent"],
        "role": fm["role"],
        "evidence_scope": fm["evidence_scope"],
        "repo_head": fm["repo_head"],
        "repo_branch": fm["repo_branch"],
        "repo_state_sha256": fm["repo_state_sha256"],
        "record_sha256": info["sha256"],
        "supersedes_event_id": fm["supersedes_event_id"],
        "project_state_changed": fm["project_state_changed"],
        "research_trace_included": fm["research_trace_included"],
        "command_inventory_included": fm["command_inventory_included"],
        "file": info["path"].relative_to(repo).as_posix(),
        "request": summarize(section(info["body"], "Request")),
        "verification": summarize(section(info["body"], "Verification")),
    }


def render_views(
    repo: Path, infos: list[dict[str, Any]], include_handoff: bool = True
) -> tuple[bytes, bytes | None]:
    rows = [history_row(repo, info) for info in infos]
    rows.sort(key=lambda row: (row["recorded_at"], row["conversation_id"], row["sequence"], row["event_id"]))
    history = "".join(json.dumps(row, ensure_ascii=False, separators=(",", ":")) + "\n" for row in rows).encode("utf-8")
    if len(history) > MAX_GENERATED_VIEW_BYTES:
        raise CollabError(f"generated history exceeds the {MAX_GENERATED_VIEW_BYTES}-byte budget")
    if not include_handoff:
        return history, None
    if not infos:
        handoff = (
            "# Start the next conversation here\n\n"
            "Generated from sealed records; do not edit.\n\n"
            "No finalized collaboration records exist yet.\n"
        ).encode("utf-8")
        return history, handoff
    latest = max(
        infos,
        key=lambda info: (
            str(info["frontmatter"]["recorded_at"]),
            str(info["frontmatter"]["finalized_at"]),
            str(info["frontmatter"]["event_id"]),
        ),
    )
    fm = latest["frontmatter"]
    handoff_body = section(latest["body"], "Handoff") or ""
    lines = [
        "# Start the next conversation here",
        "",
        "Generated from sealed records; do not edit.",
        "",
        f"Source event: `{fm['event_id']}`",
        f"Conversation: `{fm['conversation_id']}` sequence {fm['sequence']}",
        f"Finalized: {fm['finalized_at']}",
        f"Repository: branch `{fm['repo_branch']}`, HEAD `{fm['repo_head']}`",
        f"Evidence: `{fm['evidence_scope']}` / `{fm['repo_state_sha256']}`",
        "",
    ]
    for name in HANDOFF_SECTIONS:
        lines.extend([f"## {name}", "", section(handoff_body, name, level=3) or "none", ""])
    handoff = ("\n".join(lines).rstrip() + "\n").encode("utf-8")
    if len(handoff) > MAX_GENERATED_VIEW_BYTES:
        raise CollabError(f"generated handoff exceeds the {MAX_GENERATED_VIEW_BYTES}-byte budget")
    return history, handoff


def _build_views_unlocked(repo: Path) -> tuple[int, Path, Path]:
    paths = ensure_layout(repo)
    config = load_config(repo)
    infos = final_record_infos(repo, require_integrity=True)
    history, handoff = render_views(repo, infos, include_handoff=config["generated_handoff"])
    atomic_write(paths["history"], history)
    if handoff is not None:
        atomic_write(paths["handoff"], handoff)
    print(f"built {len(infos)} record(s): {paths['history']}")
    if handoff is not None:
        print(f"built handoff: {paths['handoff']}")
    return len(infos), paths["history"], paths["handoff"]


def build_views(repo: Path) -> tuple[int, Path, Path]:
    config = load_config(repo)
    if not config["conversation_records"]:
        return build_history_only(repo)
    paths = ensure_layout(repo)
    with record_lock(paths["lock"]):
        return _build_views_unlocked(repo)


def build_history_only(repo: Path) -> tuple[int, Path, Path]:
    paths = layout(repo)
    validate_repo_root(repo)
    assert_no_layout_symlinks(
        repo, [paths["agents"], paths["canonical_history"], paths["history_lock"]]
    )
    paths["agents"].mkdir(parents=True, exist_ok=True)
    with record_lock(paths["history_lock"]):
        paths, entries = ensure_history_store(repo)
    print(f"initialized canonical history with {len(entries)} event(s): {paths['canonical_history']}")
    return len(entries), paths["canonical_history"], paths["canonical_history"]


def _finalize_record_unlocked(
    repo: Path,
    record_value: str,
    evidence_scope: str,
    project_state_changed: bool,
    supersedes: str | None,
) -> Path:
    path = resolve_record(repo, record_value)
    frontmatter, body, _ = load_record(path)
    draft_problems = metadata_problems(frontmatter, path, final=False) + validate_body(body)
    if draft_problems:
        raise CollabError("cannot finalize: " + "; ".join(draft_problems))
    event_id = frontmatter["event_id"]
    research_path = layout(repo)["research"] / f"{event_id}.md"
    commands_path = layout(repo)["commands"] / f"{event_id}.md"
    research_problems: list[str] = []
    command_problems: list[str] = []
    research_raw = None
    commands_raw = None
    if frontmatter["research_trace_included"]:
        research_problems, research_raw = validate_research_trace(research_path, event_id)
    if frontmatter["command_inventory_included"]:
        command_problems, commands_raw = validate_command_inventory(commands_path, event_id)
    companion_problems = [f"research: {problem}" for problem in research_problems]
    companion_problems += [f"commands: {problem}" for problem in command_problems]
    if companion_problems:
        raise CollabError("cannot finalize: " + "; ".join(companion_problems))
    if any(
        scan_secret_text(text)
        for text in (
            render_document(frontmatter, body),
            research_raw.decode("utf-8") if research_raw is not None else "",
            commands_raw.decode("utf-8") if commands_raw is not None else "",
        )
    ):
        raise CollabError("cannot finalize an event bundle containing possible secrets")
    if supersedes is not None:
        if not uuid4_text(supersedes):
            raise CollabError("--supersedes must be a canonical lowercase UUIDv4")
        frontmatter["supersedes_event_id"] = supersedes
    frontmatter["project_state_changed"] = bool(project_state_changed or frontmatter["project_state_changed"])
    head, branch, state_digest = repository_identity(repo, evidence_scope)
    frontmatter.update(
        {
            "finalized_at": now_rfc3339(),
            "status": "final",
            "repo_head": head,
            "repo_branch": branch,
            "repo_state_sha256": state_digest,
            "evidence_scope": evidence_scope,
        }
    )
    final_text = render_document(frontmatter, body)
    final_raw = final_text.encode("utf-8")
    final_info = record_info(path, frontmatter, body, final_raw)
    final_problems = validate_final_record(final_info)
    if final_problems:
        raise CollabError("cannot finalize: " + "; ".join(final_problems))
    finalized_event_ids = set()
    for existing in list_records(repo):
        if existing.resolve() == path:
            continue
        try:
            existing_fm, _, _ = load_record(existing)
        except CollabError as exc:
            raise CollabError(f"cannot finalize while {existing.name} is invalid: {exc}") from exc
        if existing_fm.get("status") == "final":
            finalized_event_ids.add(existing_fm.get("event_id"))
    target = frontmatter["supersedes_event_id"]
    if target != "none" and target not in finalized_event_ids:
        raise CollabError(f"supersedes_event_id does not identify an existing finalized record: {target}")
    atomic_write(path, final_raw)
    seal = layout(repo)["seals"] / f"{frontmatter['event_id']}.sha256"
    bundle_sha256 = event_bundle_sha256(final_raw, research_raw, commands_raw)
    atomic_write(seal, (bundle_sha256 + "\n").encode("ascii"))
    _build_views_unlocked(repo)
    print(f"finalized and sealed: {path}")
    return path


def finalize_record(
    repo: Path,
    record_value: str,
    evidence_scope: str,
    project_state_changed: bool,
    supersedes: str | None,
) -> Path:
    config = load_config(repo)
    if not config["conversation_records"]:
        return finalize_history_event(
            repo, record_value, evidence_scope, project_state_changed, supersedes
        )
    resolved_record = resolve_record(repo, record_value)
    paths = ensure_layout(repo)
    with record_lock(paths["lock"]):
        return _finalize_record_unlocked(
            repo,
            str(resolved_record),
            evidence_scope,
            project_state_changed,
            supersedes,
        )


def finalize_history_event(
    repo: Path,
    event_id: str,
    evidence_scope: str,
    project_state_changed: bool,
    supersedes: str | None,
) -> Path:
    if not uuid4_text(event_id):
        raise CollabError("history-only finalization requires the event UUID printed by new")
    paths = layout(repo)
    with record_lock(paths["history_lock"]):
        paths, entries = ensure_history_store(repo)
        matches = [entry for entry in entries if entry.get("event_id") == event_id]
        if len(matches) != 1:
            raise CollabError(f"expected exactly one history entry for event {event_id}")
        entry = matches[0]
        draft_problems = history_entry_problems(entry, final=False)
        if draft_problems:
            raise CollabError("cannot finalize: " + "; ".join(draft_problems))
        final_ids = {
            value.get("event_id")
            for value in entries
            if value.get("status") == "final" and uuid4_text(value.get("event_id"))
        }
        if supersedes is not None:
            if not uuid4_text(supersedes):
                raise CollabError("--supersedes must be a canonical lowercase UUIDv4")
            entry["supersedes_event_id"] = supersedes
        target = entry["supersedes_event_id"]
        if target != "none" and target not in final_ids:
            raise CollabError(f"supersedes_event_id does not identify an existing final entry: {target}")
        head, branch, state_digest = repository_identity(repo, evidence_scope)
        entry.update(
            {
                "finalized_at": now_rfc3339(),
                "status": "final",
                "repo_head": head,
                "repo_branch": branch,
                "repo_state_sha256": state_digest,
                "evidence_scope": evidence_scope,
                "project_state_changed": bool(
                    project_state_changed or entry["project_state_changed"]
                ),
            }
        )
        entry["entry_sha256"] = entry_sha256(entry)
        final_problems = history_entry_problems(entry, final=True)
        if final_problems:
            raise CollabError("cannot finalize: " + "; ".join(final_problems))
        serialized = history_document_bytes(entries)
        if scan_secret_text(serialized.decode("utf-8")):
            raise CollabError("cannot finalize canonical history containing possible secrets")
        atomic_write(paths["canonical_history"], serialized)
    print(f"finalized history event: {event_id}")
    return paths["canonical_history"]


def lint_repo(repo: Path, strict: bool = False) -> int:
    config = load_config(repo)
    if not config["conversation_records"]:
        return lint_history_only(repo, strict=strict)
    paths = layout(repo)
    findings: list[tuple[str, str, str, str]] = []
    layout_safe = True

    def add(severity: str, code: str, path: Path | None, detail: str) -> None:
        location = "-"
        if path is not None:
            try:
                location = path.relative_to(repo).as_posix()
            except ValueError:
                location = str(path)
        findings.append((severity, code, location, detail))

    try:
        assert_no_layout_symlinks(
            repo,
            [
                paths["agents"],
                paths["ledger"],
                paths["records"],
                paths["research"],
                paths["commands"],
                paths["seals"],
                paths["views"],
            ],
        )
    except CollabError as exc:
        add("E", "layout", None, str(exc))
        layout_safe = False
    if layout_safe and not paths["agents"].is_dir():
        add("E", "layout", paths["agents"], "missing .agents directory; run build to initialize")

    if layout_safe and paths["agents"].is_dir():
        scanned_files = 0
        scanned_bytes = 0
        for path in paths["agents"].rglob("*"):
            if path.is_symlink():
                add("E", "layout", path, "symlinks are not allowed under .agents")
                continue
            if not path.is_file():
                continue
            scanned_files += 1
            if scanned_files > MAX_SCAN_FILES:
                add("E", "budget", paths["agents"], f"secret scan exceeds {MAX_SCAN_FILES} files")
                break
            try:
                size = path.stat().st_size
                if size > MAX_TEXT_FILE_BYTES:
                    add("E", "budget", path, f"file exceeds the {MAX_TEXT_FILE_BYTES}-byte secret-scan limit")
                    continue
                scanned_bytes += size
                if scanned_bytes > MAX_SCAN_TOTAL_BYTES:
                    add("E", "budget", paths["agents"], f"secret scan exceeds {MAX_SCAN_TOTAL_BYTES} total bytes")
                    break
                text = path.read_text(encoding="utf-8", errors="replace")
            except OSError as exc:
                add("E", "secrets", path, f"could not read file: {exc}")
                continue
            for line_no, label in scan_secret_text(text):
                add("E", "secrets", path, f"possible {label} at line {line_no}; content not printed")

    record_infos: list[dict[str, Any]] = []
    final_infos: list[dict[str, Any]] = []
    record_integrity_clean = True
    event_owners: dict[str, list[Path]] = {}
    event_features: dict[str, tuple[bool, bool]] = {}
    pair_owners: dict[tuple[str, int], list[Path]] = {}
    sequences: dict[str, list[int]] = {}

    if not layout_safe:
        record_integrity_clean = False
    elif not paths["records"].is_dir():
        add("E", "layout", paths["records"], "missing records directory; run build to initialize")
        record_integrity_clean = False
    else:
        record_entries = sorted(paths["records"].iterdir())
        if len(record_entries) > MAX_RECORDS:
            add("E", "budget", paths["records"], f"record count exceeds {MAX_RECORDS}")
            record_integrity_clean = False
            record_entries = record_entries[:MAX_RECORDS]
        for path in record_entries:
            if path.is_symlink():
                add("E", "layout", path, "record files may not be symlinks")
                record_integrity_clean = False
                continue
            if not path.is_file():
                continue
            if path.suffix != ".md":
                add("E", "naming", path, "records directory may contain only Markdown record files")
                record_integrity_clean = False
                continue
            try:
                frontmatter, body, raw = load_record(path)
            except (OSError, CollabError) as exc:
                add("E", "record", path, str(exc))
                record_integrity_clean = False
                continue
            info = record_info(path, frontmatter, body, raw)
            record_infos.append(info)
            status = frontmatter.get("status")
            is_final = status == "final"
            if status == "draft":
                add("E", "draft", path, "unfinished draft; complete and finalize it")
                record_integrity_clean = False
            problems = metadata_problems(frontmatter, path, final=is_final)
            if is_final:
                problems += validate_body(body)
            for problem in problems:
                add("E", "record", path, problem)
                record_integrity_clean = False
            event_id = frontmatter.get("event_id")
            cid = frontmatter.get("conversation_id")
            seq = frontmatter.get("sequence")
            if isinstance(event_id, str):
                event_owners.setdefault(event_id, []).append(path)
                event_features[event_id] = (
                    frontmatter.get("research_trace_included") is True,
                    frontmatter.get("command_inventory_included") is True,
                )
            if isinstance(cid, str) and isinstance(seq, int):
                pair_owners.setdefault((cid, seq), []).append(path)
                sequences.setdefault(cid, []).append(seq)
            companion_clean = True
            research_raw = None
            commands_raw = None
            if isinstance(event_id, str) and uuid4_text(event_id):
                research_path = paths["research"] / f"{event_id}.md"
                command_path = paths["commands"] / f"{event_id}.md"
                research_problems: list[str] = []
                command_problems: list[str] = []
                if frontmatter.get("research_trace_included") is True:
                    research_problems, research_raw = validate_research_trace(research_path, event_id)
                if frontmatter.get("command_inventory_included") is True:
                    command_problems, commands_raw = validate_command_inventory(command_path, event_id)
                for problem in research_problems:
                    add("E", "research", research_path, problem)
                    companion_clean = False
                for problem in command_problems:
                    add("E", "commands", command_path, problem)
                    companion_clean = False
            else:
                companion_clean = False
            if is_final and not problems and companion_clean:
                seal = paths["seals"] / f"{event_id}.sha256"
                try:
                    actual = read_seal(seal)
                except CollabError as exc:
                    add("E", "seal", seal, str(exc))
                    actual = ""
                expected = event_bundle_sha256(raw, research_raw, commands_raw)
                if actual != expected:
                    add("E", "seal", path, "missing seal or finalized event content changed")
                    record_integrity_clean = False
                else:
                    final_infos.append(info)
            elif is_final:
                record_integrity_clean = False

    for event_id, owners in event_owners.items():
        if len(owners) > 1:
            names = ", ".join(path.name for path in owners)
            for path in owners:
                add("E", "identity", path, f"event_id {event_id} is used by multiple records: {names}")
            record_integrity_clean = False
    for (cid, seq), owners in pair_owners.items():
        if len(owners) > 1:
            names = ", ".join(path.name for path in owners)
            for path in owners:
                add("E", "identity", path, f"conversation sequence {seq} is duplicated for {cid}: {names}")
            record_integrity_clean = False
    for cid, seqs in sequences.items():
        unique = sorted(set(seqs))
        if unique:
            missing = sorted(set(range(1, unique[-1] + 1)) - set(unique))
            if missing:
                add("E", "sequence", None, f"conversation {cid} is missing sequence(s): {missing}")
                record_integrity_clean = False

    known_event_ids = set(event_owners)
    supersedes_graph: dict[str, str] = {}
    for info in record_infos:
        fm = info["frontmatter"]
        event_id = fm.get("event_id")
        target = fm.get("supersedes_event_id")
        if isinstance(event_id, str) and isinstance(target, str) and target != "none":
            if target not in known_event_ids:
                add("E", "correction", info["path"], f"supersedes unknown event {target}")
                record_integrity_clean = False
            supersedes_graph[event_id] = target
    for start in supersedes_graph:
        seen: set[str] = set()
        current = start
        while current in supersedes_graph:
            if current in seen:
                owner = event_owners.get(start, [None])[0]
                add("E", "correction", owner, "supersession chain contains a cycle")
                record_integrity_clean = False
                break
            seen.add(current)
            current = supersedes_graph[current]

    if layout_safe and paths["seals"].is_dir():
        expected_seals = {f"{event_id}.sha256" for event_id in event_owners}
        for seal in sorted(paths["seals"].glob("*.sha256")):
            if seal.is_symlink():
                add("E", "layout", seal, "seal files may not be symlinks")
                record_integrity_clean = False
                continue
            if seal.name not in expected_seals:
                add("E", "seal", seal, "orphan seal has no record")
                record_integrity_clean = False

    for code, key, singular in (
        ("research", "research", "research trace"),
        ("commands", "commands", "command inventory"),
    ):
        companion_dir = paths[key]
        if not layout_safe or not companion_dir.is_dir():
            continue
        entries = sorted(companion_dir.iterdir())
        if len(entries) > MAX_RECORDS:
            add("E", "budget", companion_dir, f"{singular} count exceeds {MAX_RECORDS}")
            record_integrity_clean = False
            entries = entries[:MAX_RECORDS]
        for path in entries:
            if path.is_symlink():
                add("E", "layout", path, f"{singular} files may not be symlinks")
                record_integrity_clean = False
                continue
            if not path.is_file():
                continue
            if path.suffix != ".md" or not uuid4_text(path.stem):
                add("E", code, path, f"{singular} filename must be <record-event-id>.md")
                record_integrity_clean = False
                continue
            if path.stem not in known_event_ids:
                add("E", code, path, f"orphan {singular}: no conversation record has event {path.stem}")
                record_integrity_clean = False
                continue
            research_enabled, commands_enabled = event_features.get(path.stem, (False, False))
            enabled = research_enabled if code == "research" else commands_enabled
            if not enabled:
                add("E", code, path, f"unexpected {singular}: feature was disabled for this event")
                record_integrity_clean = False

    if record_integrity_clean:
        expected_history, expected_handoff = render_views(
            repo, final_infos, include_handoff=config["generated_handoff"]
        )
        expected_views = [("history", paths["history"], expected_history)]
        if expected_handoff is not None:
            expected_views.append(("handoff", paths["handoff"], expected_handoff))
        for name, path, expected in expected_views:
            try:
                if path.is_symlink():
                    raise CollabError("generated views may not be symlinks")
                if path.stat().st_size > MAX_GENERATED_VIEW_BYTES:
                    raise CollabError(f"generated view exceeds {MAX_GENERATED_VIEW_BYTES} bytes")
                actual = path.read_bytes()
            except CollabError as exc:
                add("E", "view", path, str(exc))
                continue
            except OSError:
                actual = None
            if actual is None:
                add("E", "view", path, f"generated {name} view is missing; run build")
            elif actual != expected:
                add("E", "view", path, f"generated {name} view is stale or hand-edited; run build")

    legacy_count = 0
    for legacy_dir in (
        ()
        if not layout_safe
        else (
            paths["agents"] / "conversations",
            paths["agents"] / "research",
            paths["agents"] / "researches",
            paths["agents"] / "commands",
        )
    ):
        if legacy_dir.is_dir():
            legacy_count += sum(1 for path in legacy_dir.iterdir() if path.is_file())
    if legacy_count:
        add("I", "legacy", None, f"{legacy_count} legacy file(s) preserved and structurally unjudged")

    findings.sort(key=lambda item: ({"E": 0, "W": 1, "I": 2}[item[0]], item[1], item[2], item[3]))
    print(f"collab lint: {repo}")
    for severity, code, location, detail in findings:
        label = {"E": "ERROR", "W": "warning", "I": "info"}[severity]
        print(f"  {label:<7} {code:<11} {location}: {detail}")
    errors = sum(1 for finding in findings if finding[0] == "E")
    warnings = sum(1 for finding in findings if finding[0] == "W")
    if not errors and not warnings:
        print("  clean.")
    print(f"  result: {errors} error(s), {warnings} warning(s), {len(final_infos)} final record(s)")
    return 1 if errors or (strict and warnings) else 0


def lint_history_only(repo: Path, strict: bool = False) -> int:
    paths = layout(repo)
    findings: list[tuple[str, str, str, str]] = []

    def add(severity: str, code: str, path: Path | None, detail: str) -> None:
        location = "-"
        if path is not None:
            try:
                location = path.relative_to(repo).as_posix()
            except ValueError:
                location = str(path)
        findings.append((severity, code, location, detail))

    layout_safe = True
    try:
        assert_no_layout_symlinks(
            repo, [paths["agents"], paths["canonical_history"], paths["history_lock"]]
        )
    except CollabError as exc:
        add("E", "layout", None, str(exc))
        layout_safe = False

    if layout_safe and paths["agents"].is_dir():
        scanned_files = 0
        scanned_bytes = 0
        for path in paths["agents"].rglob("*"):
            if path.is_symlink():
                add("E", "layout", path, "symlinks are not allowed under .agents")
                continue
            if not path.is_file():
                continue
            scanned_files += 1
            if scanned_files > MAX_SCAN_FILES:
                add("E", "budget", paths["agents"], f"secret scan exceeds {MAX_SCAN_FILES} files")
                break
            try:
                size = path.stat().st_size
                if size > MAX_TEXT_FILE_BYTES:
                    add("E", "budget", path, f"file exceeds the {MAX_TEXT_FILE_BYTES}-byte secret-scan limit")
                    continue
                scanned_bytes += size
                if scanned_bytes > MAX_SCAN_TOTAL_BYTES:
                    add("E", "budget", paths["agents"], f"secret scan exceeds {MAX_SCAN_TOTAL_BYTES} total bytes")
                    break
                text = path.read_text(encoding="utf-8", errors="replace")
            except OSError as exc:
                add("E", "secrets", path, f"could not read file: {exc}")
                continue
            for line_no, label in scan_secret_text(text):
                add("E", "secrets", path, f"possible {label} at line {line_no}; content not printed")

    entries: list[dict[str, Any]] = []
    if not layout_safe:
        pass
    elif not paths["canonical_history"].is_file():
        add("E", "history", paths["canonical_history"], "missing canonical history; run build")
    else:
        try:
            entries = load_history_document(paths["canonical_history"])
        except CollabError as exc:
            add("E", "history", paths["canonical_history"], str(exc))

    event_owners: dict[str, list[int]] = {}
    pair_owners: dict[tuple[str, int], list[int]] = {}
    sequences: dict[str, list[int]] = {}
    supersedes_graph: dict[str, str] = {}
    final_ids: set[str] = set()
    for index, entry in enumerate(entries):
        status = entry.get("status")
        is_final = status == "final"
        problems = history_entry_problems(entry, final=is_final)
        if status == "draft":
            add("E", "draft", paths["canonical_history"], f"entry {index} is unfinished")
        for problem in problems:
            add("E", "history", paths["canonical_history"], f"entry {index}: {problem}")
        event_id = entry.get("event_id")
        conversation_id = entry.get("conversation_id")
        sequence = entry.get("sequence")
        if isinstance(event_id, str):
            event_owners.setdefault(event_id, []).append(index)
        if isinstance(conversation_id, str) and isinstance(sequence, int):
            pair_owners.setdefault((conversation_id, sequence), []).append(index)
            sequences.setdefault(conversation_id, []).append(sequence)
        if is_final and not problems:
            final_ids.add(event_id)
            if entry.get("entry_sha256") != entry_sha256(entry):
                add("E", "integrity", paths["canonical_history"], f"entry {index} content changed after finalization")
        target = entry.get("supersedes_event_id")
        if isinstance(event_id, str) and isinstance(target, str) and target != "none":
            supersedes_graph[event_id] = target

    for event_id, owners in event_owners.items():
        if len(owners) > 1:
            add("E", "identity", paths["canonical_history"], f"event_id {event_id} occurs at entries {owners}")
    for (conversation_id, sequence), owners in pair_owners.items():
        if len(owners) > 1:
            add(
                "E",
                "identity",
                paths["canonical_history"],
                f"conversation {conversation_id} sequence {sequence} occurs at entries {owners}",
            )
    for conversation_id, values in sequences.items():
        unique = sorted(set(values))
        if unique:
            missing = sorted(set(range(1, unique[-1] + 1)) - set(unique))
            if missing:
                add("E", "sequence", paths["canonical_history"], f"conversation {conversation_id} is missing sequence(s): {missing}")
    for event_id, target in supersedes_graph.items():
        if target not in final_ids:
            add("E", "correction", paths["canonical_history"], f"event {event_id} supersedes unknown final event {target}")
    for start in supersedes_graph:
        seen: set[str] = set()
        current = start
        while current in supersedes_graph:
            if current in seen:
                add("E", "correction", paths["canonical_history"], f"supersession chain from {start} contains a cycle")
                break
            seen.add(current)
            current = supersedes_graph[current]

    if layout_safe and paths["ledger"].exists():
        add("I", "legacy", paths["ledger"], "bundle-mode records are preserved and structurally unjudged in history-only mode")

    findings.sort(key=lambda item: ({"E": 0, "W": 1, "I": 2}[item[0]], item[1], item[2], item[3]))
    print(f"collab lint (history-only): {repo}")
    for severity, code, location, detail in findings:
        label = {"E": "ERROR", "W": "warning", "I": "info"}[severity]
        print(f"  {label:<7} {code:<11} {location}: {detail}")
    errors = sum(1 for finding in findings if finding[0] == "E")
    warnings = sum(1 for finding in findings if finding[0] == "W")
    if not errors and not warnings:
        print("  clean.")
    print(f"  result: {errors} error(s), {warnings} warning(s), {len(final_ids)} final event(s)")
    return 1 if errors or (strict and warnings) else 0


def parser() -> argparse.ArgumentParser:
    root = argparse.ArgumentParser(description="agent collaboration ledger tooling")
    sub = root.add_subparsers(dest="command", required=True)

    new = sub.add_parser("new", help="create the configured concurrency-safe event draft(s)")
    new.add_argument("repo")
    new.add_argument("topic")
    new.add_argument("--agent", required=True)
    new.add_argument("--role", required=True, choices=sorted(ROLES))
    new.add_argument("--conversation", help="existing actual conversation UUIDv4")

    finalize = sub.add_parser("finalize", help="validate and finalize the configured event")
    finalize.add_argument("repo")
    finalize.add_argument("target", help="bundle record path or history-only event UUID")
    finalize.add_argument("--evidence-scope", required=True)
    finalize.add_argument("--project-state-changed", action="store_true")
    finalize.add_argument("--supersedes")

    build = sub.add_parser("build", help="initialize or rebuild the configured store")
    build.add_argument("repo")

    lint = sub.add_parser(
        "lint", help="validate the configured store, integrity, and secrets"
    )
    lint.add_argument("repo")
    lint.add_argument("--strict", action="store_true", help="warnings also fail")
    return root


def main(argv: list[str] | None = None) -> int:
    args = parser().parse_args(argv)
    repo = Path(args.repo).resolve()
    try:
        if args.command == "new":
            create_record(repo, args.topic, args.agent, args.role, args.conversation)
            return 0
        if args.command == "finalize":
            finalize_record(
                repo,
                args.target,
                args.evidence_scope,
                args.project_state_changed,
                args.supersedes,
            )
            return 0
        if args.command == "build":
            build_views(repo)
            return 0
        if args.command == "lint":
            return lint_repo(repo, strict=args.strict)
    except (CollabError, OSError) as exc:
        print(f"collab: {exc}", file=sys.stderr)
        return 2
    return 2


if __name__ == "__main__":
    sys.exit("Use scripts/collab.py for Sama; this module supplies shared engine functions.")

#!/usr/bin/env python3
"""Portable standard-library client for stable WindowsAgent end-user APIs."""

from __future__ import annotations

import argparse
import base64
import json
import os
import pathlib
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request


MAX_JSON_BYTES = 1024 * 1024
TERMINAL_STATES = {"COMPLETED", "FAILED", "CANCELLED"}


class ClientError(RuntimeError):
    pass


def parser() -> argparse.ArgumentParser:
    root = argparse.ArgumentParser()
    root.add_argument("--url", required=True)
    root.add_argument("--timeout", type=float, default=30.0)
    commands = root.add_subparsers(dest="command", required=True)

    commands.add_parser("health")
    capture = commands.add_parser("capture")
    capture.add_argument("--output-dir", type=pathlib.Path)
    capture.add_argument("--no-cursor", action="store_true")
    capture.add_argument(
        "--profile",
        choices=("native-jpeg", "1080p-jpeg", "native-png"),
        default="native-jpeg",
    )

    execute = commands.add_parser("exec")
    execute_commands = execute.add_subparsers(dest="operation", required=True)
    for operation in ("run", "start", "ps1"):
        operation_parser = execute_commands.add_parser(operation)
        operation_parser.add_argument("--executable")
        operation_parser.add_argument("--script-path")
        operation_parser.add_argument("--arg", action="append", default=[])
        operation_parser.add_argument("--env", action="append", default=[])
        operation_parser.add_argument("--cwd")
        operation_parser.add_argument("--stdin-file", type=pathlib.Path)
        operation_parser.add_argument("--window", choices=("normal", "hidden"))
        operation_parser.add_argument("--max-output-bytes", type=int)
        operation_parser.add_argument("--execution-timeout", type=float)

    key = commands.add_parser("key")
    key_commands = key.add_subparsers(dest="key_operation", required=True)
    press = key_commands.add_parser("press")
    press.add_argument("--key", required=True)
    press.add_argument("--hold-ms", type=int, required=True)
    press.add_argument("--expected-process-id", type=int, required=True)
    press.add_argument("--expected-executable-name", required=True)
    press.add_argument("--expected-executable-path", required=True)
    return root


def normalize_base_url(value: str) -> str:
    parsed = urllib.parse.urlsplit(value)
    if (
        parsed.scheme != "http"
        or not parsed.hostname
        or parsed.username
        or parsed.password
        or parsed.path not in ("", "/")
        or parsed.query
        or parsed.fragment
    ):
        raise ClientError("--url must be one credential-free HTTP origin")
    return urllib.parse.urlunsplit((parsed.scheme, parsed.netloc, "", "", "")).rstrip("/")


def opener() -> urllib.request.OpenerDirector:
    return urllib.request.build_opener(urllib.request.ProxyHandler({}))


def read_json(response, context: str):
    body = response.read(MAX_JSON_BYTES + 1)
    if len(body) > MAX_JSON_BYTES:
        raise ClientError(f"{context} returned oversized JSON")
    try:
        return json.loads(body)
    except (UnicodeDecodeError, json.JSONDecodeError) as error:
        raise ClientError(f"{context} returned malformed JSON: {error}") from error


def request(client, method: str, url: str, timeout: float, context: str, payload=None):
    data = None
    headers = {}
    if payload is not None:
        data = json.dumps(payload, separators=(",", ":")).encode("utf-8")
        headers["Content-Type"] = "application/json"
    message = urllib.request.Request(url, data=data, headers=headers, method=method)
    try:
        return client.open(message, timeout=timeout)
    except urllib.error.HTTPError as error:
        body = error.read(MAX_JSON_BYTES + 1)
        detail = body.decode("utf-8", errors="replace").strip()
        raise ClientError(f"{context} failed with HTTP {error.code}: {detail}") from error
    except urllib.error.URLError as error:
        raise ClientError(f"{context} could not reach WindowsAgent: {error.reason}") from error


def absolute_url(origin: str, location: str) -> str:
    resolved = urllib.parse.urljoin(origin + "/", location)
    if urllib.parse.urlsplit(resolved).netloc != urllib.parse.urlsplit(origin).netloc:
        raise ClientError("WindowsAgent returned a cross-origin operation URL")
    return resolved


def health(client, origin: str, timeout: float):
    with request(client, "GET", origin + "/healthz", timeout, "health check") as response:
        result = read_json(response, "health check")
    if not isinstance(result, dict) or result.get("status") != "ok":
        raise ClientError("health check did not return status ok")
    return result


def capture(client, origin: str, timeout: float, args):
    health(client, origin, timeout)
    payload = {"include_cursor": not args.no_cursor, "profile": args.profile}
    with request(client, "POST", origin + "/v1/captures", timeout, "capture", payload) as response:
        if response.status != 201:
            raise ClientError(f"capture returned HTTP {response.status}, expected 201")
        metadata = read_json(response, "capture")
    if not isinstance(metadata, dict) or not isinstance(metadata.get("content_url"), str):
        raise ClientError("capture metadata omitted content_url")
    with request(
        client,
        "GET",
        absolute_url(origin, metadata["content_url"]),
        timeout,
        "capture download",
    ) as response:
        content = response.read()
    extension = {"jpeg": ".jpg", "png": ".png"}.get(metadata.get("format"))
    if extension is None:
        raise ClientError("capture metadata returned an unsupported format")
    output_dir = (args.output_dir or pathlib.Path.cwd() / "windowsagent-captures").expanduser().resolve()
    output_dir.mkdir(parents=True, exist_ok=True)
    descriptor, path = tempfile.mkstemp(prefix="windowsagent-", suffix=extension, dir=output_dir)
    with os.fdopen(descriptor, "wb") as output:
        output.write(content)
        output.flush()
        os.fsync(output.fileno())
    result = dict(metadata)
    result["image_path"] = path
    return result


def parse_environment(entries: list[str]) -> dict[str, str]:
    result = {}
    casefolded = set()
    for entry in entries:
        if "=" not in entry or entry.startswith("="):
            raise ClientError("--env must be NAME=VALUE")
        name, value = entry.split("=", 1)
        folded = name.casefold()
        if folded in casefolded:
            raise ClientError(f"duplicate environment name: {name}")
        casefolded.add(folded)
        result[name] = value
    return result


def execution_payload(args):
    operation = "powershell-file" if args.operation == "ps1" else args.operation
    payload = {"schemaVersion": 1, "operation": operation}
    if operation == "powershell-file":
        if not args.script_path or args.executable:
            raise ClientError("exec ps1 requires --script-path and does not accept --executable")
        payload["scriptPath"] = args.script_path
    else:
        if not args.executable or args.script_path:
            raise ClientError(f"exec {operation} requires --executable and does not accept --script-path")
        payload["executable"] = args.executable
    if args.arg:
        payload["argv"] = args.arg
    environment = parse_environment(args.env)
    if environment:
        payload["env"] = environment
    if args.cwd:
        payload["cwd"] = args.cwd
    if args.stdin_file:
        payload["stdin"] = base64.b64encode(args.stdin_file.read_bytes()).decode("ascii")
    if args.window:
        payload["window"] = args.window
    if args.max_output_bytes is not None:
        if args.max_output_bytes < 0:
            raise ClientError("--max-output-bytes must not be negative")
        payload["maxOutputBytes"] = args.max_output_bytes
    if args.execution_timeout is not None:
        milliseconds = args.execution_timeout * 1000
        if args.execution_timeout < 0 or not milliseconds.is_integer():
            raise ClientError("--execution-timeout must resolve to whole milliseconds")
        payload["timeoutMilliseconds"] = int(milliseconds)
    return payload


def wait_for_invocation(client, origin: str, timeout: float, invoke_path: str, payload):
    with request(client, "POST", origin + invoke_path, timeout, "invoke", payload) as response:
        if response.status != 202:
            raise ClientError(f"invoke returned HTTP {response.status}, expected 202")
        accepted = read_json(response, "invoke")
        location = response.headers.get("Location")
    if not isinstance(accepted, dict) or not isinstance(accepted.get("invocationId"), str) or not location:
        raise ClientError("invoke response omitted invocation identity or Location")
    invocation_id = accepted["invocationId"]
    status_url = absolute_url(origin, location)
    stop = accepted.get("stop")
    stop_url = None
    if isinstance(stop, dict) and stop.get("method") == "POST" and isinstance(stop.get("url"), str):
        stop_url = absolute_url(origin, stop["url"])
    try:
        while True:
            with request(client, "GET", status_url, timeout, "invocation status") as response:
                status = read_json(response, "invocation status")
            if not isinstance(status, dict) or status.get("invocationId") != invocation_id:
                raise ClientError("invocation status identity changed")
            state = status.get("state")
            if state in TERMINAL_STATES:
                if state != "COMPLETED":
                    raise ClientError(json.dumps(status, separators=(",", ":")))
                return status
            if state not in ("RUNNING", "CANCELLING"):
                raise ClientError(f"invocation returned invalid state {state!r}")
            time.sleep(0.2)
    except KeyboardInterrupt:
        if stop_url:
            with request(client, "POST", stop_url, timeout, "cancel invocation"):
                pass
        raise


def direct_key_payload(args):
    if args.hold_ms <= 0 or args.expected_process_id <= 0:
        raise ClientError("key press requires positive hold and process ID values")
    return {
        "schemaVersion": 1,
        "expectedForeground": {
            "processId": args.expected_process_id,
            "executableName": args.expected_executable_name,
            "executablePath": args.expected_executable_path,
        },
        "key": args.key,
        "holdMs": args.hold_ms,
    }


def main() -> int:
    args = parser().parse_args()
    if args.timeout <= 0:
        raise ClientError("--timeout must be positive")
    origin = normalize_base_url(args.url)
    client = opener()
    if args.command == "health":
        result = health(client, origin, args.timeout)
    elif args.command == "capture":
        result = capture(client, origin, args.timeout, args)
    elif args.command == "exec":
        result = wait_for_invocation(
            client, origin, args.timeout, "/v1/executions/invoke", execution_payload(args)
        )
    else:
        result = wait_for_invocation(
            client, origin, args.timeout, "/v1/key-inputs/invoke", direct_key_payload(args)
        )
    print(json.dumps(result, separators=(",", ":")))
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except ClientError as error:
        raise SystemExit(f"error: {error}") from error

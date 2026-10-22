#!/usr/bin/env python3
"""Seed a disposable loopback review server. Never targets a production URL."""

import argparse
import datetime
import hashlib
import json
from pathlib import Path
import sqlite3
import time
import urllib.error
import urllib.parse
import urllib.request


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base-url", default="http://127.0.0.1:18081")
    parser.add_argument("--db", type=Path, required=True)
    parser.add_argument("--downloads", type=Path, required=True)
    parser.add_argument("--api-snapshot", type=Path)
    parser.add_argument("--compare-api", type=Path)
    args = parser.parse_args()
    url = urllib.parse.urlparse(args.base_url)
    if url.scheme != "http" or url.hostname not in ("127.0.0.1", "localhost", "::1"):
        parser.error("only a disposable HTTP loopback server is supported")

    def call(path, body=None, token="review-only-token"):
        request = urllib.request.Request(
            args.base_url.rstrip("/") + path,
            data=None if body is None else json.dumps(body).encode(),
            headers={"Authorization": "Bearer " + token, "Content-Type": "application/json"},
        )
        with urllib.request.urlopen(request, timeout=10) as response:
            return json.load(response)

    for attempt in range(50):
        try:
            call("/health/live")
            break
        except urllib.error.URLError:
            if attempt == 49:
                raise
            time.sleep(0.1)
    if args.compare_api:
        baseline = json.loads(args.compare_api.read_text())
        for path, expected in baseline.items():
            if call(path) != expected:
                raise AssertionError("API response changed: " + path)
        print(f"All {len(baseline)} baseline API responses are identical.")
        return
    if call("/api/v1/devices")["devices"]:
        parser.error("the review server must have an empty, disposable database")

    ids = set()
    for n, name in enumerate(("리뷰 데스크톱", "리뷰 노트북")):
        device = call("/api/v1/devices", {"name": name})
        ids.add(device["id"])
        events = []
        for i in range(120):
            source = ("claude_code", "codex", "antigravity")[i % 3]
            model = {"claude_code": "claude-haiku-5-5", "codex": "gpt-6.1-sol", "antigravity": "gemini-3.8-flash"}[source]
            timestamp = datetime.datetime(2026, 10, 22, tzinfo=datetime.timezone.utc) + datetime.timedelta(minutes=i * 12 + n)
            events.append({
                "event_id": hashlib.sha256(f"review-{n}-{i}".encode()).hexdigest(),
                "device_id": device["id"], "source": source, "ts": timestamp.isoformat(),
                "model": "unpriced-review-model" if i % 13 == 0 else model,
                "session_id": f"review-session-{i // 10}", "project": f"/workspace/project-{i % 3}",
                "input": 1000 + i, "output": 200 + i, "cache_read": 400,
                "cache_write_5m": 100 if source == "claude_code" else 0, "cache_write_1h": 0,
                "cache_write": 0 if source == "claude_code" else 100,
                "reasoning": 50, "confidence": "exact", "tool_version": "review",
            })
        call("/api/v1/ingest", {"device": {
            "id": device["id"], "name": ("review-desktop", "review-laptop")[n],
            "os": ("linux", "darwin")[n], "arch": "arm64", "agent_version": "review-fixture",
        }, "events": events}, device["token"])

    if not args.db.is_file():
        parser.error("the supplied database must be the running server's existing database")
    with sqlite3.connect(args.db) as db:
        if {row[0] for row in db.execute("SELECT id FROM devices")} != ids:
            parser.error("the supplied database is not the isolated review fixture")
        fixed = 1792713600000
        db.execute("UPDATE devices SET token_created_at=?, last_seen=?, last_ingest=?", (fixed, fixed, fixed))
        db.execute("UPDATE usage_events SET received_at=?", (fixed,))

    args.downloads.mkdir(parents=True, exist_ok=True)
    (args.downloads / "manifest.json").write_text(json.dumps({
        "version": "review-fixture", "built_at": "2026-10-23T00:00:00Z",
        "files": [{"name": f"ai-usage-agent_{os}_{arch}" + (".exe" if os == "windows" else ""),
                   "os": os, "arch": arch, "size": 4194304, "sha256": "a" * 64}
                  for os in ("linux", "darwin", "windows") for arch in ("amd64", "arm64")],
    }))
    if args.api_snapshot:
        query = "from=2026-10-20&to=2026-10-24&tz=Asia%2FSeoul"
        paths = ["/api/v1/devices", "/api/v1/pricing", "/api/v1/summary?" + query,
                 "/api/v1/events?" + query + "&limit=100"]
        for group in ("source", "device", "model", "project", "session"):
            paths += [f"/api/v1/{endpoint}?{query}&group_by={group}" + ("&bucket=day" if endpoint == "timeseries" else "")
                      for endpoint in ("breakdown", "timeseries", "throughput")]
        args.api_snapshot.write_text(json.dumps({path: call(path) for path in paths}))
    print("Prepared 240 synthetic events, two devices and a dummy download manifest.")


if __name__ == "__main__":
    main()

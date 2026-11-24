#!/usr/bin/env python3
"""Bounded profile inspection through the authenticated application API."""
import argparse
import concurrent.futures
import json
import os
import threading
import time
import urllib.error
import urllib.request


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--chat", type=int, required=True)
    parser.add_argument("--mode", choices=["seed", "preview", "apply"], default="preview")
    parser.add_argument("--scope", choices=["all", "left"], default="all")
    parser.add_argument("--report")
    parser.add_argument("--output", required=True)
    args = parser.parse_args()
    if args.chat >= 0 or (args.mode == "apply" and not args.report):
        parser.error("negative chat ID required; apply also requires --report")
    token = os.environ.get("ADMIN_API_TOKEN", "")
    if not token:
        parser.error("ADMIN_API_TOKEN is required")
    gate = threading.Lock()
    next_request = [0.0]

    class NoRedirect(urllib.request.HTTPRedirectHandler):
        def redirect_request(self, *args, **kwargs):
            return None

    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())

    def api(path, body=None):
        with gate:
            time.sleep(max(0, next_request[0] - time.monotonic()))
            next_request[0] = time.monotonic() + 0.7
        data = None if body is None else json.dumps(body).encode()
        req = urllib.request.Request("http://127.0.0.1:8080" + path, data=data,
                                     headers={"Authorization": "Bearer " + token, "Content-Type": "application/json"})
        try:
            with opener.open(req, timeout=40) as response:
                return json.load(response)
        except urllib.error.HTTPError as exc:
            # Never print request headers, endpoint credentials or raw responses.
            raise RuntimeError("API HTTP " + str(exc.code)) from None
        except (urllib.error.URLError, TimeoutError):
            raise RuntimeError("API connection failed") from None

    prefix = "/api/v1/groups/" + str(args.chat)
    if args.mode == "apply":
        with open(args.report, encoding="utf-8") as report:
            rows = [json.loads(line) for line in report if line.strip()]
        if any(row.get("chat_id") != args.chat or row.get("mode") != "preview" for row in rows):
            parser.error("apply requires a preview report for the same chat")
        users = sorted({int(r["user_id"]) for r in rows if r.get("matched") and r.get("present") and not r.get("protected")})
    else:
        users, before = [], ""
        while True:
            page = api(prefix + "/users" + ("?before=" + before if before else ""))
            users.extend(int(row["user_id"]) for row in page if args.scope == "all" or row.get("left_at"))
            if len(page) < 100:
                break
            before = str(page[-1]["user_id"])
        users = sorted(set(users))
    print(json.dumps({"phase": args.mode, "candidates": len(users)}), flush=True)

    def inspect(uid):
        row = {"chat_id": args.chat, "mode": args.mode, "user_id": uid}
        try:
            row.update(api(prefix + "/profile-check", {"user_id": uid, "learn": args.mode == "seed", "apply": args.mode == "apply"}))
        except Exception as exc:
            row["error"] = str(exc) if isinstance(exc, RuntimeError) else "inspection failed"
        return row

    counts = {"checked": 0, "matched": 0, "present_matched": 0, "applied": 0, "learned": 0, "errors": 0, "unavailable": 0}
    fd = os.open(args.output, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, "w", encoding="utf-8") as out, concurrent.futures.ThreadPoolExecutor(max_workers=3) as pool:
        for row in pool.map(inspect, users):
            out.write(json.dumps(row, ensure_ascii=False) + "\n")
            out.flush()
            counts["checked"] += 1
            for key in ("matched", "applied", "learned"):
                counts[key] += bool(row.get(key))
            counts["present_matched"] += bool(row.get("matched") and row.get("present"))
            counts["errors"] += bool(row.get("error"))
            counts["unavailable"] += not row.get("protected", False) and not row.get("bio_available", False)
            if counts["checked"] % 25 == 0:
                print(json.dumps(counts), flush=True)
    print(json.dumps(counts), flush=True)


if __name__ == "__main__":
    main()

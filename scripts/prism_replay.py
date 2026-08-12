#!/usr/bin/env python3
# Replay a recorded request corpus against a Prism mock server.
#
# Usage:
#     python3 scripts/prism_replay.py [corpus.json] [base_url]
#
# Exits non-zero if any recorded request cannot be resolved by Prism.
# Writes a JSON coverage report to prism-coverage.json.
import json
import sys
import urllib.request
import urllib.error


def main():
    corpus_path = sys.argv[1] if len(sys.argv) > 1 else "tests/pact/prism-replay-corpus.json"
    base_url = (sys.argv[2] if len(sys.argv) > 2 else "http://localhost:4010").rstrip("/")

    with open(corpus_path, "r", encoding="utf-8") as f:
        corpus = json.load(f)

    report = {"total": len(corpus), "resolved": 0, "unresolved": [], "results": []}
    for entry in corpus:
        method = entry.get("method", "GET").upper()
        path = entry.get("path", "/")
        url = base_url + path
        req = urllib.request.Request(url, method=method)
        try:
            with urllib.request.urlopen(req, timeout=15) as resp:
                status = resp.status
                body = resp.read().decode("utf-8", "replace")
        except urllib.error.HTTPError as e:
            status = e.code
            body = e.read().decode("utf-8", "replace")
        except Exception as e:
            status = -1
            body = str(e)

        prism_error = "stoplight.io/prism/errors" in body
        resolved = (status < 400) and not prism_error

        item = {"method": method, "path": path, "status": status, "resolved": resolved}
        report["results"].append(item)
        if resolved:
            report["resolved"] += 1
        else:
            report["unresolved"].append({"method": method, "path": path, "status": status})

    with open("prism-coverage.json", "w", encoding="utf-8") as f:
        json.dump(report, f, indent=2)

    print("Prism replay coverage: %d/%d resolved" % (report["resolved"], report["total"]))
    for u in report["unresolved"]:
        print("  UNRESOLVED: %s %s (status %s)" % (u["method"], u["path"], u["status"]))

    if report["unresolved"]:
        sys.exit(1)


if __name__ == "__main__":
    main()

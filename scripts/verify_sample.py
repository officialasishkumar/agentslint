#!/usr/bin/env python3
"""Check a random sample of missing-path errors against git history.

For each sampled finding, ask the GitHub commits API whether the path ever
existed in the repository (relative to the instruction file's directory, then
to the root). A path with history was real once and has since been moved or
deleted: the instruction drifted. Usage:

    GITHUB_TOKEN=... python3 scripts/verify_sample.py census.jsonl 200 > verify.tsv
"""
import json
import os
import posixpath
import random
import sys
import time
import urllib.parse
import urllib.request

census, n = sys.argv[1], int(sys.argv[2])
token = os.environ.get("GITHUB_TOKEN") or os.environ["GH_TOKEN"]
pool = []
for line in open(census):
    r = json.loads(line)
    for d in r.get("docs") or []:
        for f in d["findings"]:
            if f["rule"] == "missing-path" and f["severity"] == "error" and not f["text"].startswith(("cd ", "python", "node", "pytest", "bash", "sh ")):
                pool.append((r["repo"], d["path"], f["line"], f["text"]))
random.seed(42)
sample = random.sample(pool, min(n, len(pool)))


def history(repo, p):
    q = urllib.parse.urlencode({"path": p, "per_page": 1})
    req = urllib.request.Request(f"https://api.github.com/repos/{repo}/commits?{q}",
                                 headers={"Authorization": f"Bearer {token}", "Accept": "application/vnd.github+json"})
    for attempt in range(4):
        try:
            with urllib.request.urlopen(req, timeout=30) as resp:
                commits = json.load(resp)
                if commits:
                    return commits[0]["commit"]["committer"]["date"][:10]
                return ""
        except Exception as e:  # noqa: BLE001
            time.sleep(5 * (attempt + 1))
    return "?"


print("repo\tfile\tline\tref\tlast_commit_touching_path\tverdict")
counts = {"existed (moved/deleted)": 0, "no history at that path": 0, "?": 0}
for repo, doc, line, ref in sample:
    ref_clean = ref.strip("./").rstrip("/") if not ref.startswith("../") else ref.rstrip("/")
    candidates = [posixpath.normpath(posixpath.join(posixpath.dirname(doc), ref.rstrip("/"))), ref_clean]
    when = ""
    for c in dict.fromkeys(candidates):
        if c.startswith(".."):
            continue
        when = history(repo, c)
        if when:
            break
    verdict = "?" if when == "?" else ("existed (moved/deleted)" if when else "no history at that path")
    counts[verdict] += 1
    print(f"{repo}\t{doc}\t{line}\t{ref}\t{when}\t{verdict}")
    sys.stdout.flush()
print(json.dumps({"pool": len(pool), "sampled": len(sample), **counts}), file=sys.stderr)

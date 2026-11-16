#!/usr/bin/env python3
"""amh LoCoMo retrieval benchmark (BETA).

Loads the LoCoMo multi-session conversation dataset, splits conversations into
sentence-level memories, inserts them into a temporary amh store, then queries
with category-1 (single-hop) questions and reports recall@k.

Scope note: retrieval recall only — no temporal reasoning or multi-hop scoring.
Results are NOT comparable to memhub's aggregate LoCoMo scores.

Usage:
    python3 locomo_recall.py --top-k 5 [--limit 5]

Requires: pip install datasets (first run downloads ~100MB).
"""
import argparse
import json
import os
import sqlite3
import sys
import tempfile
import time
import uuid

TOP_K = 5
SPLIT_NAME = "en"

MIGRATION = """
CREATE TABLE IF NOT EXISTS memories (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL CHECK(type IN ('decision','gotcha','preference','project-rule','handoff','note')),
    content TEXT NOT NULL,
    project_key TEXT NOT NULL DEFAULT '',
    source_agent TEXT NOT NULL DEFAULT '',
    trust TEXT NOT NULL CHECK(trust IN ('explicit','extracted','user')),
    status TEXT NOT NULL CHECK(status IN ('active','proposed','retired')),
    superseded_by TEXT NOT NULL DEFAULT '',
    first_seen TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_memories_project_status ON memories(project_key, status);
CREATE INDEX IF NOT EXISTS idx_memories_type ON memories(type);
"""


def utc(ts=None):
    return time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(ts or time.time()))


def load_loco(limit):
    try:
        from datasets import load_dataset
    except ImportError:
        sys.exit("locomo_recall: pip install datasets first")
    ds = load_dataset("snap-research/locomo10", split=f"test_{SPLIT_NAME}")
    return [ds[i] for i in range(min(limit, len(ds)))]


def insert_memories(conn, conv_id, utterances):
    now = utc()
    rows = []
    seen = set()
    for utt in utterances:
        speaker = utt.get("speaker", "?")
        text = (utt.get("text") or "").strip()
        for sent in text.replace("! ", ".\n").replace("? ", ".\n").split("\n"):
            sent = sent.strip()
            if len(sent) < 30 or sent in seen:
                continue
            seen.add(sent)
            rows.append((uuid.uuid4().hex, "note", f"{speaker}: {sent}",
                         f"locomo/conv-{conv_id}", "locomo", "explicit", "active", "", now, now))
    conn.executemany(
        "INSERT OR IGNORE INTO memories(id,type,content,project_key,source_agent,trust,status,superseded_by,first_seen,created_at)"
        " VALUES(?,?,?,?,?,?,?,?,?,?)", rows)
    return len(rows)


def recall_at_k(conn, question, session, conv_id, k=TOP_K):
    toks = [w for w in question.lower().split() if len(w) >= 3][:8]
    if not toks:
        return 0
    where = " AND ".join(["(content LIKE ? ESCAPE '\\' OR content LIKE ? ESCAPE '\\')"] * len(toks))
    params = []
    for t in toks:
        params += [f"%{t}%", f"%{t[0].upper() + t[1:]}%"]
    rows = conn.execute(
        f"SELECT content FROM memories WHERE status='active' AND project_key=? AND {where} LIMIT {k}",
        [f"locomo/conv-{conv_id}"] + params).fetchall()
    ans = (session.get("answer") or "").strip()
    if not ans:
        return 0
    for (content,) in rows:
        if ans.lower() in content.lower() or content.lower() in ans.lower():
            return 1
    return 0


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--top-k", type=int, default=TOP_K)
    ap.add_argument("--limit", type=int, default=5, help="number of LoCoMo conversations (max 10)")
    args = ap.parse_args()

    convos = load_loco(args.limit)
    total, hits = 0, 0
    with tempfile.TemporaryDirectory(prefix="amh-locomo-") as td:
        db = sqlite3.connect(os.path.join(td, "amh.db"))
        db.executescript(MIGRATION)
        for conv in convos:
            conv_id = conv.get("conversation_id", conv.get("sample_id", "x"))
            session = conv.get("conversation", conv)
            obs = session.get("session_1", {}).get("utterances", []) if isinstance(session, dict) else []
            n = insert_memories(db, conv_id, obs)
            qa = conv.get("event_qa") or conv.get("qa") or []
            for q in qa:
                if str(q.get("category", q.get("question_category", ""))) != "1":
                    continue
                total += 1
                hits += recall_at_k(db, q.get("question", ""), q, conv_id, args.top_k)
            print(f"locomo conv {conv_id}: {n} memories, cumulative recall@{args.top_k} = {hits}/{total}")
        db.close()
    if total == 0:
        sys.exit("locomo_recall: no category-1 questions found (dataset schema drift? check BETA note)")
    print(f"\nlocomo_recall (BETA): recall@{args.top_k} = {hits}/{total} = {hits/total:.3f}")


if __name__ == "__main__":
    main()

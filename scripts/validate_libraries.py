#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""doc-library 固定验收脚本（仅开发验收，生产工具为纯 Go，不经 Python 桥接）。

用法（Conda base Python，仅标准库）：
    /home/lingnc/miniconda3/bin/python scripts/validate_libraries.py            # 静态预检
    /home/lingnc/miniconda3/bin/python scripts/validate_libraries.py --runtime  # 静态+运行时（需 build/doc-library）

运行时部分以 subprocess 以绝对路径调用 CLI 二进制 + 绝对配置路径；cwd 可与仓库不同。
每次 CLI 调用记录退出码、耗时、response.cache、warnings，不盲写 PASS。
产物只写入 build/validation/，预览图在 build/validation/预览/。
PDF 命中按真实结果记录，未命中/无图记 WARN 并原样保留 warnings，不伪造。
缓存持久/多进程与源码边界由 integration 测试 owner 覆盖，此处不重复合成单测。
"""

import argparse
import hashlib
import json
import os
import re
import subprocess
import sys
import time
from datetime import datetime, timezone

REPO = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
CONFIG = os.path.join(REPO, "examples", "libraries.yaml")
BIN = os.path.join(REPO, "build", "doc-library")
OUT = os.path.join(REPO, "build", "validation")
PREVIEW = os.path.join(OUT, "预览")
PYTHON = "/home/lingnc/miniconda3/bin/python"

# 真实三源根（只读接入，不复制文档，不全 workspace 扫描）
SOURCES = {
    "yantrace": os.path.normpath(os.path.join(REPO, "..", "yantrace")),
    "examzh": os.path.normpath(os.path.join(REPO, "..", "yantrace", "vendor", "exam-zh-0.3.6", "exam-zh")),
    "luadraw": os.path.normpath(os.path.join(REPO, "..", "compare-figure", "vendor", "luadraw-3.7", "luadraw")),
}

CHECKS = []  # (name, status, detail)
PHASE = "static"


def record(name, status, detail=""):
    CHECKS.append({"name": name, "phase": PHASE, "status": status, "detail": detail})
    print(f"[{status}] {name}: {detail}")


class Fail(Exception):
    pass


def run_cli(args, req=None, timeout=600, cwd="/tmp", use_stdin=True, config=True):
    """CLI二进制和配置用绝对路径；schema可显式不加载配置。"""
    cmd = [BIN]
    if config:
        cmd += ["--config", CONFIG]
    cmd += list(args)
    input_bytes = None
    if req is not None:
        cmd += ["--stdin"] if use_stdin else []
        input_bytes = json.dumps(req, ensure_ascii=False).encode("utf-8")
    t0 = time.monotonic()
    proc = subprocess.run(cmd, input=input_bytes, cwd=cwd, timeout=timeout,
                          stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    dt = time.monotonic() - t0
    out = proc.stdout.decode("utf-8", "replace")
    entry = {"cmd": cmd, "cwd": cwd, "rc": proc.returncode, "seconds": round(dt, 3),
             "stderr": proc.stderr.decode("utf-8", "replace")[:2000]}
    try:
        entry["response"] = json.loads(out)
    except Exception:
        entry["stdout_raw"] = out[:2000]
    return entry


def resp_ok(entry, name):
    r = entry.get("response")
    if r is None:
        raise Fail(f"{name}: 无 JSON 响应 rc={entry['rc']} stderr={entry.get('stderr','')[:300]}")
    if not r.get("ok"):
        raise Fail(f"{name}: ok=false error={r.get('error')} rc={entry['rc']}")
    if r.get("version") != "doc-library/1":
        raise Fail(f"{name}: version={r.get('version')}")
    return r


def resp_err(entry, name):
    r = entry.get("response")
    if r is None:
        raise Fail(f"{name}: 期望错误 JSON 但无响应 rc={entry['rc']}")
    if r.get("ok") is not False or not r.get("error"):
        raise Fail(f"{name}: 期望 ok:false+error，实际 {json.dumps(r, ensure_ascii=False)[:200]}")
    return r


def cache_summary(r):
    c = r.get("cache") or {}
    return {k: c.get(k) for k in ("hits", "misses", "files", "namespace", "fingerprint")}


def valid_source_hits(results, expected_source, query):
    """Check source identity, confined file path, and one-based source line."""
    parts = [part for part in query.split() if part]
    hits = [x for x in results if x.get("kind") == "source"
            and all(part in (x.get("text") or "") for part in parts)]
    for hit in hits:
        root = os.path.realpath(SOURCES[expected_source])
        path = os.path.realpath(os.path.join(root, hit.get("doc", "")))
        if hit.get("source") != expected_source or os.path.commonpath([root, path]) != root:
            return [], f"source/path escape: {hit.get('source')}:{hit.get('doc')}"
        if not os.path.isfile(path) or int(hit.get("start_line") or 0) < 1:
            return [], f"missing file or invalid line: {hit.get('doc')}:{hit.get('start_line')}"
    return hits, ""


# ---------------------------------------------------------------- 静态预检

def static_checks():
    if os.path.isfile(CONFIG):
        record("config_exists", "PASS", CONFIG)
    else:
        record("config_exists", "FAIL", f"缺少 {CONFIG}")
        return
    text = open(CONFIG, encoding="utf-8").read()
    ids = re.findall(r"^\s*-\s*id:\s*(\S+)", text, re.M)
    if sorted(ids) == ["examzh", "luadraw", "yantrace"]:
        record("config_sources", "PASS", f"三源 id 齐全: {ids}")
    else:
        record("config_sources", "FAIL", f"三源 id 异常: {ids}")
    for sid, root in SOURCES.items():
        if os.path.isdir(root):
            record(f"source_root_{sid}", "PASS", root)
        else:
            record(f"source_root_{sid}", "FAIL", f"真实资料根不存在: {root}")
    for tool in ("pdftotext", "pdftoppm"):
        found = any(os.access(os.path.join(d, tool), os.X_OK)
                    for d in os.environ.get("PATH", "").split(os.pathsep))
        record(f"tool_{tool}", "PASS" if found else "FAIL", "PATH 中可用" if found else "未找到")
    req = os.path.join(REPO, "examples", "requests.jsonl")
    n_ok = 0
    if os.path.isfile(req):
        for i, line in enumerate(open(req, encoding="utf-8"), 1):
            line = line.strip()
            if not line:
                continue
            try:
                json.loads(line)
                n_ok += 1
            except Exception as e:
                record(f"jsonl_line{i}", "FAIL", str(e))
        record("jsonl_examples", "PASS" if n_ok >= 4 else "FAIL", f"{n_ok} 条合法请求")
    else:
        record("jsonl_examples", "FAIL", f"缺少 {req}")
    for p in ("types.go", "library.go", "engine.go", "cmd/doc-library/main.go"):
        record(f"src_{p}", "PASS" if os.path.isfile(os.path.join(REPO, p)) else "FAIL", p)


# ---------------------------------------------------------------- 运行时验收

def runtime_checks(pdf_page=1):
    global PHASE
    PHASE = "runtime"
    os.makedirs(OUT, exist_ok=True)
    os.makedirs(PREVIEW, exist_ok=True)
    stats = {"runs": []}

    def note(name, entry, extra=""):
        r = entry.get("response") or {}
        stats["runs"].append({"name": name, "rc": entry["rc"], "seconds": entry["seconds"],
                              "cache": cache_summary(r) if r.get("ok") else None,
                              "warnings": r.get("warnings"), "extra": extra})

    # 1. schema 模式：不要求资料/PDF 工具
    e = run_cli(["--mode", "schema"], config=False)
    r = resp_ok(e, "schema")
    if r.get("schema"):
        record("mode_schema", "PASS", "返回 schema 且无 root")
    else:
        record("mode_schema", "FAIL", "schema 为空")
    note("schema", e)

    # 2. status：仅元数据扫描，不抽取（cache.files>0 但不应有 misses 全量抽取压力）
    e = run_cli(["--stdin"], {"mode": "status", "limit": 200})
    r = resp_ok(e, "status")
    n_files = sum(1 for x in r.get("results", []) if x.get("kind") in ("file", "directory"))
    record("mode_status", "PASS" if n_files > 0 else "FAIL",
           f"列出 {n_files} 个文件, cache={cache_summary(r)}")
    note("status", e)

    e = run_cli(["--stdin"], {"mode": "list", "source": "yantrace", "doc": "doc", "limit": 20})
    try:
        r = resp_ok(e, "list")
        results = r.get("results") or []
        record("mode_list", "PASS" if results and all(x.get("source") == "yantrace" for x in results) else "FAIL",
               f"{len(results)} 条目录/文件结果")
    except Fail as ex:
        record("mode_list", "FAIL", str(ex)[:200])
    note("list", e)

    e = run_cli(["--stdin"], {"mode": "find", "source": "examzh", "query": "00-minimal.tex", "limit": 5})
    try:
        r = resp_ok(e, "find")
        results = r.get("results") or []
        good = any(x.get("source") == "examzh" and x.get("doc") == "examples/basic/00-minimal.tex" for x in results)
        record("mode_find", "PASS" if good else "WARN", f"匹配文件={good}, hits={len(results)}")
    except Fail as ex:
        record("mode_find", "FAIL", str(ex)[:200])
    note("find", e)

    # 3. search yantrace
    e = run_cli(["--stdin"], {"mode": "search", "query": "LRSheetCompose", "source": "yantrace",
                              "kinds": ["source"], "limit": 5})
    r = resp_ok(e, "search_lrsheetcompose")
    hits, problem = valid_source_hits(r.get("results", []), "yantrace", "LRSheetCompose")
    if hits and not problem and all(x.get("source") == "yantrace" for x in r["results"]):
        record("search_yantrace_lrsheetcompose", "PASS",
               f"{len(hits)} 命中, 首条 {hits[0]['doc']}:{hits[0].get('start_line')}, cache={cache_summary(r)}")
    else:
        record("search_yantrace_lrsheetcompose", "FAIL" if problem else "WARN", problem or f"hits={len(hits)} results={len(r.get('results', []))}")
    note("search_lrsheetcompose", e)

    # 4. 混合/纯中文查询（源码，不触发 PDF）
    for label, q, src in [("mixed", "中括号 LRCompactAnswerCard", "yantrace"),
                          ("two_chinese_terms", "题号 作答框", "yantrace"),
                          ("choices", "choices", "examzh"), ("fillin", "fillin", "examzh")]:
        e = run_cli(["--stdin"], {"mode": "search", "query": q, "source": src, "kinds": ["source"]})
        try:
            r = resp_ok(e, f"search_{label}")
            hits, problem = valid_source_hits(r.get("results", []), src, q)
            record(f"search_{label}", "PASS" if hits and not problem else ("FAIL" if problem else "WARN"),
                   problem or f"query={q!r} source={src} hits={len(hits)}")
        except Fail as ex:
            record(f"search_{label}", "WARN", str(ex)[:200])
        note(f"search_{label}", e)

    # 5. luadraw grep Dpolyline3d / Dscene3d
    for q in ("Dpolyline3d", "Dscene3d"):
        e = run_cli(["--stdin"], {"mode": "grep", "query": q, "source": "luadraw", "kinds": ["source"], "recursive": True, "limit": 5})
        try:
            r = resp_ok(e, f"grep_{q}")
            hits, problem = valid_source_hits(r.get("results", []), "luadraw", q)
            record(f"grep_luadraw_{q}", "PASS" if hits and not problem else ("FAIL" if problem else "WARN"),
                   problem or (f"{len(hits)} 行命中, 首条 {hits[0]['doc']}:{hits[0].get('start_line')}" if hits else "无命中"))
        except Fail as ex:
            record(f"grep_luadraw_{q}", "WARN", str(ex)[:200])
        note(f"grep_{q}", e)

    # 6. read 精读：exact hit 定位 + 段落返回
    e = run_cli(["--stdin"], {"mode": "search", "query": "LRSheetCompose", "source": "yantrace",
                              "kinds": ["source"], "limit": 1})
    r = resp_ok(e, "search_for_read")
    if r.get("results"):
        h0 = r["results"][0]
        e2 = run_cli(["--stdin"], {"mode": "read", "source": h0["source"], "doc": h0["doc"],
                                   "start_line": h0.get("start_line", 1),
                                   "end_line": (h0.get("start_line") or 1) + 10})
        r2 = resp_ok(e2, "read_hit")
        seg = r2.get("results") or []
        ok = seg and "LRSheetCompose" in (seg[0].get("text") or "")
        record("read_exact_hit", "PASS" if ok else "FAIL",
               f"read {h0['doc']}:{h0.get('start_line')} 回段含查询词={ok}, cache={cache_summary(r2)}")
        note("read_hit", e2)
    else:
        record("read_exact_hit", "WARN", "前置 search 无命中，跳过")

    # 7. 边界与错误路径（预期错误，退出码 2）
    for name, reqobj in [
        ("reject_parent_dir", {"mode": "read", "source": "yantrace", "doc": "../yantrace/README.md"}),
        ("reject_abs_path", {"mode": "read", "source": "yantrace", "doc": "/etc/hostname"}),
        ("reject_excluded", {"mode": "read", "source": "yantrace", "doc": "README.md"}),
        ("reject_unknown_source", {"mode": "search", "query": "x", "source": "nosuch"}),
        ("reject_view_outdir_in_root", {"mode": "view", "source": "yantrace", "doc": "doc/yantrace.pdf",
                                        "page": 1, "out_dir": SOURCES["yantrace"]}),
    ]:
        e = run_cli(["--stdin"], reqobj)
        try:
            resp_err(e, name)
            record(name, "PASS", f"拒绝并返回 error rc={e['rc']}")
        except Fail as ex:
            record(name, "FAIL", str(ex)[:200])

    # 8. 提取缓存用主动搜索验证；频次只在被动摘要之间比较。
    cache_request = {"mode": "search", "source": "yantrace", "query": "LRSheetCompose",
                     "kinds": ["source"], "limit": 2}
    e1 = run_cli([], cache_request)
    c1 = cache_summary(resp_ok(e1, "cache_run1"))
    e2 = run_cli([], cache_request)
    c2 = cache_summary(resp_ok(e2, "cache_run2"))
    ok = c2.get("hits", 0) > 0 and c2.get("misses", 0) == 0 and c1.get("files") == c2.get("files")
    record("cache_reuse", "PASS" if ok else "FAIL",
           f"run1={c1} run2={c2}（命中仍读取源文件校验SHA）")
    note("cache_run1", e1); note("cache_run2", e2)
    freq_path = os.path.join(REPO, "build", "doc-cache")
    freq_files = [os.path.join(dp, "frequency-v1.json") for dp, _, fs in os.walk(freq_path)
                  if "frequency-v1.json" in fs] if os.path.isdir(freq_path) else []
    mtime0 = {f: os.stat(f).st_mtime_ns for f in freq_files}
    for _ in range(2):
        passive = run_cli([], {"mode": "frequent", "limit": 5, "passive": True})
        resp_ok(passive, "frequent_passive")
    mtime1 = {f: os.stat(f).st_mtime_ns for f in freq_files}
    same = mtime0 == mtime1
    record("frequency_mtime_stable", "PASS" if same else "FAIL",
           "两次frequent passive未重写频次mtime_ns" if same else f"mtime变化: {mtime0} -> {mtime1}")

    # 9. frequent 模式（passive 摘要）
    e = run_cli(["--stdin"], {"mode": "frequent", "passive": True, "limit": 5})
    try:
        r = resp_ok(e, "frequent")
        record("mode_frequent", "PASS", f"{len(r.get('results', []))} 条高频条目")
    except Fail as ex:
        record("mode_frequent", "WARN", str(ex)[:200])
    note("frequent", e)

    # 10. JSONL：示例 4 条 + 1 条非法 + 2 条有效，错误行后继续，退出 1
    lines = open(os.path.join(REPO, "examples", "requests.jsonl"), encoding="utf-8").read().rstrip("\n")
    payload = lines + '\n{ invalid json !!\n' \
        + '{"mode":"search","query":"LRSheetCompose","source":"yantrace","kinds":["source"],"limit":1}\n' \
        + '{"mode":"frequent","passive":true,"limit":1}\n'
    cmd = [BIN, "--config", CONFIG, "--jsonl"]
    t0 = time.monotonic()
    proc = subprocess.run(cmd, input=payload.encode("utf-8"), cwd="/tmp", timeout=900,
                          stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    dt = time.monotonic() - t0
    out_lines = [l for l in proc.stdout.decode("utf-8", "replace").splitlines() if l.strip()]
    parsed = []
    for l in out_lines:
        try:
            parsed.append(json.loads(l))
        except Exception:
            parsed.append(None)
    n_err = sum(1 for p in parsed if p and p.get("ok") is False)
    n_okl = sum(1 for p in parsed if p and p.get("ok") is True)
    ok = proc.returncode == 1 and n_err >= 1 and n_okl >= 6
    record("jsonl_mixed_stream", "PASS" if ok else "FAIL",
           f"rc={proc.returncode} ok行={n_okl} 错误行={n_err} 耗时={dt:.1f}s（非法行后继续处理）")
    stats["runs"].append({"name": "jsonl_mixed", "rc": proc.returncode, "seconds": round(dt, 2),
                          "ok_lines": n_okl, "err_lines": n_err})

    # 11. 跨目录调用：binary/config 绝对路径，cwd=/tmp，flag 形式
    e = run_cli(["--mode", "search", "--query", "LRSheetCompose",
                 "--source", "yantrace", "--kind", "source", "--limit", "3"], req=None, cwd="/tmp", use_stdin=False)
    try:
        r = resp_ok(e, "cross_cwd_flags")
        record("cross_cwd_flag_call", "PASS", f"cwd=/tmp flags 调用 hits={len(r.get('results', []))}")
    except Fail as ex:
        record("cross_cwd_flag_call", "FAIL", str(ex)[:200])
    note("cross_cwd_flags", e)

    # 12. index 模式：冷建三源（含 PDF，可能 60s+），results.text 为 file SHA
    e = run_cli(["--stdin"], {"mode": "index", "limit": 500, "max_chars": 100000}, timeout=1800)
    try:
        r = resp_ok(e, "index")
        n = len(r.get("results", []))
        sha_like = all(re.fullmatch(r"[0-9a-f]{64}", x.get("text") or "") for x in r.get("results", [])[:10])
        record("mode_index", "PASS" if n > 0 and sha_like else "WARN",
               f"返回SHA行={n}, 全源文件={cache_summary(r).get('files')}, text为SHA={sha_like}, cache={cache_summary(r)}, warnings={len(r.get('warnings') or [])}")
        for w in (r.get("warnings") or [])[:10]:
            stats["runs"].append({"name": "index_warning", "text": w})
    except Fail as ex:
        record("mode_index", "FAIL", str(ex)[:200])
    note("index", e)
    e2 = run_cli(["--stdin"], {"mode": "index", "limit": 500}, timeout=1800)
    try:
        r2 = resp_ok(e2, "index_repeat")
        c2 = cache_summary(r2)
        good = c2.get("hits", 0) > 0 and c2.get("misses", 0) == 0
        record("index_repeat_cache", "PASS" if good else "FAIL",
               f"重复全库index cache={c2}; SHA校验仍会读取源文件")
    except Fail as ex:
        record("index_repeat_cache", "FAIL", str(ex)[:200])
    note("index_repeat", e2)

    # 13. PDF search
    e = run_cli(["--stdin"], {"mode": "search", "query": "选择题", "source": "examzh",
                              "kinds": ["pdf"], "limit": 3})
    try:
        r = resp_ok(e, "pdf_search")
        hits = [x for x in r.get("results", []) if x.get("kind") == "pdf"]
        good = hits and all(x.get("page", 0) > 0 for x in hits)
        record("pdf_search_examzh_chinese", "PASS" if good else "WARN",
               f"{len(hits)} 命中, 首条 page={hits[0].get('page') if hits else '-'}" if hits else "无命中（记录 warnings，不伪造）")
        if hits:
            stats["pdf_hit"] = {"doc": hits[0]["doc"], "page": hits[0]["page"],
                                "bbox_pct": hits[0].get("bbox_pct")}
    except Fail as ex:
        record("pdf_search_examzh_chinese", "WARN", str(ex)[:200])
    note("pdf_search_examzh", e)

    # 14. PDF read：yantrace doc/yantrace.pdf 指定物理页
    e = run_cli(["--stdin"], {"mode": "read", "source": "yantrace", "doc": "doc/yantrace.pdf", "page": pdf_page})
    try:
        r = resp_ok(e, "pdf_read")
        blocks = r.get("results") or []
        record("pdf_read_yantrace", "PASS" if blocks else "WARN",
               f"page={pdf_page} 返回 {len(blocks)} 个文本块" + ("" if blocks else "（无可抽取文字，记录不伪造）"))
    except Fail as ex:
        record("pdf_read_yantrace", "WARN", str(ex)[:200])
    note("pdf_read", e)

    # 15. PDF view：固定物理页13与10/5/95/95区域，渲染PNG到build/validation/预览/
    region = {"left": 10, "top": 5, "right": 95, "bottom": 95}
    doc, page = "doc/yantrace.pdf", pdf_page
    e = run_cli(["--stdin"], {"mode": "view", "source": "yantrace", "doc": doc, "page": page,
                       "region": region, "dpi": 100, "out_dir": PREVIEW}, timeout=300)
    try:
        r = resp_ok(e, "pdf_view")
        imgs = r.get("images") or []
        if imgs:
            p = imgs[0].get("path", "")
            png_ok = os.path.isfile(p) and open(p, "rb").read(8) == b"\x89PNG\r\n\x1a\n"
            inside_preview = os.path.abspath(p).startswith(os.path.abspath(PREVIEW))
            record("pdf_view_region", "PASS" if png_ok and inside_preview else "FAIL",
                   f"{p} {imgs[0].get('width')}x{imgs[0].get('height')} png={png_ok} 位于预览目录={inside_preview}")
        else:
            record("pdf_view_region", "FAIL", "无 images 返回")
        note("pdf_view", e, extra=json.dumps(region, ensure_ascii=False))
    except Fail as ex:
        record("pdf_view_region", "FAIL", str(ex)[:200])

    return stats


# ---------------------------------------------------------------- 报告

def file_sha256(path):
    if not os.path.isfile(path):
        return None
    digest = hashlib.sha256()
    with open(path, "rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def write_report(stats):
    os.makedirs(OUT, exist_ok=True)
    by_phase = {}
    for phase in ("static", "runtime"):
        checks = [c for c in CHECKS if c.get("phase", phase) == phase]
        by_phase[phase] = {status.lower(): sum(1 for c in checks if c["status"] == status)
                           for status in ("PASS", "WARN", "FAIL")}
    total = {key: sum(by_phase[phase][key] for phase in by_phase) for key in ("pass", "warn", "fail")}
    revision = subprocess.run(["git", "rev-parse", "HEAD"], cwd=REPO, text=True,
                              stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=False).stdout.strip() or None
    report = {
        "generated_at": datetime.now(timezone.utc).isoformat(),
        "binary": BIN, "binary_exists": os.path.isfile(BIN), "binary_sha256": file_sha256(BIN),
        "config": CONFIG, "config_sha256": file_sha256(CONFIG), "git_revision": revision,
        "summary": {"static": by_phase["static"], "runtime": by_phase["runtime"], "total": total},
        "checks": CHECKS,
        "runs": (stats or {}).get("runs", []),
        "pdf_hit": (stats or {}).get("pdf_hit"),
    }
    path = os.path.join(OUT, "report.json")
    with open(path, "w", encoding="utf-8") as f:
        json.dump(report, f, ensure_ascii=False, indent=2)
    return path, total["pass"], total["warn"], total["fail"]


def main():
    global PHASE
    ap = argparse.ArgumentParser()
    ap.add_argument("--runtime", action="store_true", help="执行运行时验收（需 build/doc-library）")
    ap.add_argument("--pdf-page", type=int, default=13, help="yantrace doc/yantrace.pdf 测试物理页")
    args = ap.parse_args()

    static_checks()
    stats = None
    if args.runtime:
        PHASE = "runtime"
        if os.path.isfile(BIN) and os.access(BIN, os.X_OK):
            try:
                stats = runtime_checks(pdf_page=args.pdf_page)
            except Fail as ex:
                record("runtime_aborted", "FAIL", str(ex)[:300])
            except subprocess.TimeoutExpired as ex:
                record("runtime_aborted", "FAIL", f"超时: {ex}")
        else:
            record("binary_ready", "FAIL", f"二进制不存在或不可执行: {BIN}")
    else:
        record("runtime_skipped", "INFO", "未加 --runtime，仅静态预检")

    path, p, w, f = write_report(stats)
    print(f"\n报告: {path}\nPASS={p} WARN={w} FAIL={f}")
    return 1 if f else 0


if __name__ == "__main__":
    sys.exit(main())

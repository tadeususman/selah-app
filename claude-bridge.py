#!/usr/bin/env python3
"""
Claude Bridge — HTTP server yang menerima prompt dari Docker container
dan menjalankan `claude -p` di host, lalu mengembalikan hasilnya.
Fallback otomatis ke Groq jika Claude gagal (rate limit / quota habis).

Jalankan di host: python3 claude-bridge.py
"""

import json
import os
import subprocess
import sys
import threading
import time
from datetime import datetime, timezone
from http.server import BaseHTTPRequestHandler, HTTPServer
from socketserver import ThreadingMixIn


# ── Load .env dari direktori yang sama ──────────────────────────────────────

def _load_dotenv(path: str) -> None:
    if not os.path.exists(path):
        return
    with open(path) as f:
        for line in f:
            line = line.strip()
            if line and not line.startswith("#") and "=" in line:
                key, val = line.split("=", 1)
                os.environ.setdefault(key.strip(), val.strip())

_load_dotenv(os.path.join(os.path.dirname(os.path.abspath(__file__)), ".env"))


# ── Groq setup ───────────────────────────────────────────────────────────────

try:
    from groq import Groq as GroqClient
    _groq_available = True
except ImportError:
    _groq_available = False

GROQ_MODEL = os.environ.get("GROQ_MODEL", "qwen/qwen3.6-27b")
GROQ_API_KEY = os.environ.get("GROQ_API_KEY", "")

# Batasi jumlah proses `claude -p` yang boleh jalan bersamaan, supaya RAM
# tidak numpuk kalau ada beberapa request AI masuk berdekatan (tiap proses
# CLI bisa makan ~150-300MB). Request lain antre, bukan spawn baru.
CLAUDE_MAX_CONCURRENT = int(os.environ.get("CLAUDE_MAX_CONCURRENT", "2"))
_claude_semaphore = threading.Semaphore(CLAUDE_MAX_CONCURRENT)


# ── Server ───────────────────────────────────────────────────────────────────

class ThreadingHTTPServer(ThreadingMixIn, HTTPServer):
    daemon_threads = True

HOST = "0.0.0.0"
PORT = 8765

_LOG_FILE = os.path.join(os.path.dirname(os.path.abspath(__file__)), "usage.jsonl")
_log_lock = threading.Lock()


def _log_usage(app: str, ms: int, prompt_len: int, ok: bool, provider: str, err: str = "",
               input_tokens: int = 0, output_tokens: int = 0, cost_usd: float = 0.0) -> None:
    entry = {
        "ts": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
        "app": app or "unknown",
        "ms": ms,
        "prompt_len": prompt_len,
        "ok": ok,
        "provider": provider,
        "err": err,
        "input_tokens": input_tokens,
        "output_tokens": output_tokens,
        "cost_usd": cost_usd,
    }
    with _log_lock:
        try:
            with open(_LOG_FILE, "a") as f:
                f.write(json.dumps(entry) + "\n")
        except Exception as e:
            print(f"[bridge] log error: {e}", file=sys.stderr)


def _ask_claude(prompt: str, cwd, model) -> tuple:
    """Coba Claude CLI. Returns (output, input_tokens, output_tokens, cost_usd, success)."""
    cmd = ["claude", "-p", prompt, "--output-format", "json", "--tools", ""]
    if model:
        cmd += ["--model", model]
    try:
        with _claude_semaphore:
            result = subprocess.run(
                cmd,
                capture_output=True,
                text=True,
                timeout=360,
                cwd=cwd,
            )
        raw = result.stdout.strip()
        if result.returncode == 0 and raw:
            try:
                parsed = json.loads(raw)
                output = parsed.get("result", "").strip()
                if output:
                    usage = parsed.get("usage", {})
                    input_tok = usage.get("input_tokens", 0) + usage.get("cache_creation_input_tokens", 0) + usage.get("cache_read_input_tokens", 0)
                    output_tok = usage.get("output_tokens", 0)
                    cost = parsed.get("total_cost_usd", 0.0) or 0.0
                    return output, input_tok, output_tok, cost, True
            except Exception:
                pass
        err_detail = result.stderr.strip() or raw or "no output"
        print(f"[bridge] Claude gagal (rc={result.returncode}): {err_detail[:120]}", file=sys.stderr)
        return "", 0, 0, 0.0, False
    except subprocess.TimeoutExpired:
        print("[bridge] Claude timeout (360s)", file=sys.stderr)
        return "", 0, 0, 0.0, False
    except FileNotFoundError:
        print("[bridge] Claude CLI tidak ditemukan di PATH", file=sys.stderr)
        return "", 0, 0, 0.0, False
    except Exception as e:
        print(f"[bridge] Claude exception: {e}", file=sys.stderr)
        return "", 0, 0, 0.0, False


def _strip_think_blocks(text: str) -> str:
    """Strip <think>...</think> reasoning blocks from model output."""
    import re
    cleaned = re.sub(r"<think>.*?</think>", "", text, flags=re.DOTALL)
    return cleaned.strip()


def _ask_groq(prompt: str) -> tuple:
    """Fallback ke Groq. Returns (output, input_tokens, output_tokens, cost_usd, success)."""
    if not _groq_available or not GROQ_API_KEY:
        return "", 0, 0, 0.0, False
    try:
        client = GroqClient(api_key=GROQ_API_KEY)
        response = client.chat.completions.create(
            model=GROQ_MODEL,
            messages=[{"role": "user", "content": prompt}],
            temperature=0.3,
            max_tokens=900,
        )
        output = response.choices[0].message.content.strip()
        output = _strip_think_blocks(output)
        usage = response.usage
        input_tok = getattr(usage, "prompt_tokens", 0) or 0
        output_tok = getattr(usage, "completion_tokens", 0) or 0
        return output, input_tok, output_tok, 0.0, bool(output)
    except Exception as e:
        print(f"[bridge] Groq error: {e}", file=sys.stderr)
        return str(e), 0, 0, 0.0, False


def _compute_stats(app_filter: str = "", from_date: str = "", to_date: str = ""):
    if not os.path.exists(_LOG_FILE):
        return {"error": "usage log belum ada", "total": 0}
    entries = []
    with open(_LOG_FILE) as f:
        for line in f:
            line = line.strip()
            if line:
                try:
                    entries.append(json.loads(line))
                except Exception:
                    pass

    # Apply filters
    if app_filter:
        entries = [e for e in entries if e.get("app") == app_filter]
    if from_date:
        entries = [e for e in entries if e.get("ts", "") >= from_date]
    if to_date:
        to_end = to_date + "T23:59:59Z"
        entries = [e for e in entries if e.get("ts", "") <= to_end]

    today_str = datetime.now(timezone.utc).strftime("%Y-%m-%d")
    by_provider, by_app, today, ms_by_provider = {}, {}, {}, {}
    total_input_tokens = total_output_tokens = 0
    total_cost_usd = 0.0
    for e in entries:
        p = e.get("provider", "error")
        app = e.get("app", "unknown")
        by_provider[p] = by_provider.get(p, 0) + 1
        by_app[app] = by_app.get(app, 0) + 1
        if e.get("ok") and e.get("ms"):
            ms_by_provider.setdefault(p, []).append(e["ms"])
        if e.get("ts", "").startswith(today_str):
            today[p] = today.get(p, 0) + 1
        total_input_tokens += e.get("input_tokens", 0) or 0
        total_output_tokens += e.get("output_tokens", 0) or 0
        total_cost_usd += e.get("cost_usd", 0.0) or 0.0
    avg_ms = {p: int(sum(v) / len(v)) for p, v in ms_by_provider.items() if v}
    return {
        "total": len(entries),
        "by_provider": by_provider,
        "by_app": by_app,
        "today": today,
        "avg_ms": avg_ms,
        "recent": entries[-20:],
        "total_input_tokens": total_input_tokens,
        "total_output_tokens": total_output_tokens,
        "total_cost_usd": round(total_cost_usd, 4),
    }


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path.startswith("/stats"):
            from urllib.parse import urlparse, parse_qs
            qs = parse_qs(urlparse(self.path).query)
            app_filter = qs.get("app", [""])[0]
            from_date  = qs.get("from", [""])[0]
            to_date    = qs.get("to",   [""])[0]
            self._reply(200, _compute_stats(app_filter, from_date, to_date))
        elif self.path == "/health":
            self._reply(200, {"status": "ok"})
        else:
            self._reply(404, {"error": "not found"})

    def do_POST(self):
        if self.path != "/ask":
            self._reply(404, {"error": "not found"})
            return

        length = int(self.headers.get("Content-Length", 0))
        body = self.rfile.read(length)

        try:
            data = json.loads(body)
            prompt = data.get("prompt", "").strip()
            cwd = data.get("cwd", None)
            model = data.get("model", None)
            force_provider = data.get("provider", "").strip().lower()
        except Exception:
            self._reply(400, {"error": "invalid json"})
            return

        if not prompt:
            self._reply(400, {"error": "prompt is empty"})
            return

        if cwd and not os.path.isdir(cwd):
            self._reply(400, {"error": f"cwd tidak ditemukan: {cwd}"})
            return

        app = data.get("app", "unknown") or "unknown"
        t0 = time.time()

        input_tok = output_tok = 0
        cost_usd = 0.0

        if force_provider == "groq":
            output, input_tok, output_tok, cost_usd, ok = _ask_groq(prompt)
            provider = "groq" if ok else "error"
        else:
            output, input_tok, output_tok, cost_usd, ok = _ask_claude(prompt, cwd, model)
            provider = "claude"

            if not ok:
                print(f"[bridge] fallback ke Groq untuk {app}...", file=sys.stderr)
                output, input_tok, output_tok, cost_usd, ok = _ask_groq(prompt)
                provider = "groq" if ok else "error"

        ms = int((time.time() - t0) * 1000)

        if not ok:
            _log_usage(app, ms, len(prompt), False, provider, output or "both providers failed")
            self._reply(500, {"error": output or "Semua provider gagal."})
            return

        _log_usage(app, ms, len(prompt), True, provider,
                   input_tokens=input_tok, output_tokens=output_tok, cost_usd=cost_usd)
        self._reply(200, {"output": output, "provider": provider})

    def _reply(self, code, data):
        body = json.dumps(data).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, fmt, *args):
        print(f"[bridge] {self.address_string()} {fmt % args}", file=sys.stderr)


if __name__ == "__main__":
    groq_ready = _groq_available and bool(GROQ_API_KEY)
    if groq_ready:
        print(f"[bridge] Groq fallback: AKTIF (model: {GROQ_MODEL})", file=sys.stderr)
    else:
        reasons = []
        if not _groq_available:
            reasons.append("groq package tidak terinstall")
        if not GROQ_API_KEY:
            reasons.append("GROQ_API_KEY tidak diset")
        print(f"[bridge] Groq fallback: NONAKTIF ({', '.join(reasons)})", file=sys.stderr)

    server = ThreadingHTTPServer((HOST, PORT), Handler)
    print(f"[bridge] listening on {HOST}:{PORT}", file=sys.stderr)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        print("[bridge] stopped", file=sys.stderr)

#!/usr/bin/env python3
"""Online voice bridge for PetPC.

Publishes a GPT-SoVITS-shaped HTTP API on localhost and serves it from a public
HuggingFace Space, so PetPC needs no knowledge of the online protocol: it just
points TTSBaseURL at this process instead of at a local api_v2.py.

Why a bridge rather than teaching the Go client the Space's protocol:

  * Reusing the existing client means the retry, cache, splitting and playback
    paths already built for the local server apply unchanged - the online mode is
    a different SOURCE of WAV bytes, not a different feature.
  * The Space speaks Gradio 3.7's queue over WebSocket with a three-step
    handshake (see _synthesize_once). Implementing that in Go would mean a
    WebSocket client and a hand-rolled protocol, for no gain.
  * If the Space disappears, this file is the only thing that breaks. PetPC keeps
    working against a local api_v2.py.

Endpoints (deliberately a subset of api_v2.py):

    POST /tts                    {"text","text_lang","ref_audio_path",...} -> WAV
    GET  /set_gpt_weights        ?weights_path=<speaker>   selects the voice
    GET  /set_sovits_weights     accepted and ignored (no per-voice SoVITS here)
    GET  /voices                 the available voices, for the settings dropdown
    GET  /health                 liveness probe

Two hard limits of the upstream Space, both measured, both load-bearing:

  1. **100 characters maximum per request.** At 110 it answers
     "输入文字过长！110>100" and produces NO audio. Longer text must be split by
     the caller (PetPC already splits into ~20-character chunks).
  2. **A wrong speaker name fails silently** - the job completes with no audio
     rather than an error. So /voices is the authoritative list, and this bridge
     remembers whether a name was ever known to work.
"""

from __future__ import annotations

import argparse
import asyncio
import base64
import hashlib
import json
import os
import re
import sys
import threading
import time
import urllib.parse
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Any

try:
    import websockets  # type: ignore
except ImportError:  # pragma: no cover - startup guidance, not logic
    sys.exit(
        "missing dependency: websockets\n"
        "  install with:  python -m pip install websockets\n"
        "  (the GPT-SoVITS venv already has it: D:\\GPT-SoVITS\\.venv\\Scripts\\python.exe)"
    )

DEFAULT_HOST = "zomehwh-vits-uma-genshin-honkai.hf.space"
MAX_TEXT_CHARS = 100  # upstream refuses anything longer, with no audio

# The Space's fn_index=0 takes component ids [6,7,13,15,16,17]:
#   text, language, speaker, noise_scale, noise_scale_w, length_scale
LANG_NAMES = {"zh": "中文", "ja": "日语", "en": "中文"}


def log(msg: str) -> None:
    print(f"[{time.strftime('%H:%M:%S')}] {msg}", flush=True)


# --------------------------------------------------------------------------
# Space client
# --------------------------------------------------------------------------


class SpaceClient:
    """Talks to the Gradio 3.7 queue on one Space."""

    def __init__(self, host: str, timeout: float = 240.0) -> None:
        self.host = host
        self.timeout = timeout
        self._voices: list[str] | None = None
        # Names this process has actually produced audio for. Used to explain a
        # silent failure: a name that never worked is almost always a typo, since
        # the server does not reject unknown speakers.
        self._known_good: set[str] = set()

    # --- metadata ---------------------------------------------------------

    def fetch_voices(self) -> list[str]:
        """Reads the Speaker dropdown out of the Space's /config."""
        if self._voices is not None:
            return self._voices
        url = f"https://{self.host}/config"
        req = urllib.request.Request(url, headers={"User-Agent": "Mozilla/5.0"})
        with urllib.request.urlopen(req, timeout=30) as resp:
            cfg = json.load(resp)

        names: list[str] = []
        for comp in cfg.get("components", []):
            props = comp.get("props", {})
            if props.get("label") != "Speaker":
                continue
            for choice in props.get("choices") or []:
                # Gradio 3.x gives either "name" or ["name", "value"].
                name = choice[0] if isinstance(choice, (list, tuple)) else choice
                if isinstance(name, str) and name and name not in names:
                    names.append(name)
        self._voices = names
        return names

    def health(self) -> bool:
        try:
            url = f"https://{self.host}/config"
            req = urllib.request.Request(url, headers={"User-Agent": "Mozilla/5.0"})
            with urllib.request.urlopen(req, timeout=15) as resp:
                return resp.status == 200
        except Exception:
            return False

    # --- synthesis --------------------------------------------------------

    async def _synthesize_once(self, text: str, speaker: str, lang: str) -> str:
        """Runs one job and returns the data: URL of the WAV.

        The handshake is the part that is easy to get wrong, so it is spelled out:

          1. connect; the server immediately sends {"msg":"send_hash"}
          2. send the job -> the server replies estimation, then {"msg":"send_data"}
          3. **send the same job again** - that is what send_data is asking for
          4. the server replies process_starts, then process_completed with the
             output (a list whose [1] is the base64 WAV)

        A job that returns process_completed with an EMPTY data list means the
        speaker name was not recognised; there is no error message for it.
        """
        data = [
            text,
            LANG_NAMES.get(lang, "中文"),
            speaker,
            0.6,    # noise_scale       (expression)
            0.668,  # noise_scale_w     (phoneme duration)
            1.2,    # length_scale      (speed)
        ]
        job = {"fn_index": 0, "data": data, "session_hash": f"petpc{os.getpid()}"}
        blob = json.dumps(job)

        async with websockets.connect(
            f"wss://{self.host}/queue/join", open_timeout=30, max_size=None
        ) as ws:
            await asyncio.wait_for(ws.recv(), timeout=15)  # send_hash
            await ws.send(blob)

            resent = False
            while True:
                raw = await asyncio.wait_for(ws.recv(), timeout=self.timeout)
                try:
                    msg = json.loads(raw)
                except (TypeError, ValueError):
                    continue
                kind = msg.get("msg")

                if kind == "send_data" and not resent:
                    await ws.send(blob)
                    resent = True
                    continue

                if kind == "process_completed":
                    out = msg.get("output") or {}
                    if out.get("error"):
                        raise RuntimeError(f"server error: {out['error']}")
                    items = out.get("data") or []
                    if len(items) >= 2 and isinstance(items[1], str):
                        if items[1].startswith("data:audio"):
                            return items[1]
                    raise SilentFailure(speaker)
                # estimation / process_starts / process_generating: keep waiting.

    def synthesize(self, text: str, speaker: str, lang: str = "zh",
                   attempts: int = 3) -> bytes:
        """Synthesises text, retrying transient queue failures."""
        text = text.strip()
        if not text:
            raise ValueError("empty text")
        if len(text) > MAX_TEXT_CHARS:
            # Not truncated silently: a caller that gets half a sentence back
            # would play half a sentence, and the user would hear the pet stop
            # mid-thought with no explanation.
            raise ValueError(
                f"text is {len(text)} chars; the upstream service accepts at most "
                f"{MAX_TEXT_CHARS}"
            )

        last: Exception | None = None
        for attempt in range(1, attempts + 1):
            try:
                url = asyncio.run(self._synthesize_once(text, speaker, lang))
                raw = base64.b64decode(url.split(",", 1)[1])
                if not raw:
                    raise RuntimeError("empty audio")
                self._known_good.add(speaker)
                return raw
            except SilentFailure:
                # No point retrying: the name is simply not recognised.
                raise
            except Exception as exc:  # noqa: BLE001 - retried below
                last = exc
                if attempt < attempts:
                    log(f"  attempt {attempt}/{attempts} failed ({exc}); retrying")
                    time.sleep(1.5 * attempt)
        raise RuntimeError(f"all {attempts} attempts failed: {last}")

    def speaker_hint(self, speaker: str) -> str:
        """Explains a silent failure in terms the user can act on."""
        if speaker in self._known_good:
            return ""
        voices = self._voices or []
        if speaker not in voices:
            close = [v for v in voices if speaker and speaker[:2] in v][:5]
            hint = f"；最接近的名字：{'、'.join(close)}" if close else ""
            return (
                f"在线服务不认识「{speaker}」这个名字。"
                f"请从下拉列表里选（该服务有 {len(voices)} 个音色）{hint}"
            )
        return f"「{speaker}」没有返回音频，可能该音色在服务端不可用"


class SilentFailure(RuntimeError):
    """The job completed but produced no audio - almost always a bad speaker."""

    def __init__(self, speaker: str) -> None:
        super().__init__(f"no audio returned for speaker {speaker!r}")
        self.speaker = speaker


# --------------------------------------------------------------------------
# Response cache
# --------------------------------------------------------------------------


class Cache:
    """Content-addressed WAV cache.

    The Space takes 1-8 seconds per sentence, and the pet's fixed lines repeat
    constantly, so a cache turns those into instant playback. It is also what
    keeps the online mode from hammering a free public service.
    """

    def __init__(self, directory: str, max_mb: int = 300) -> None:
        self.dir = directory
        self.max_bytes = max_mb * 1024 * 1024
        self._lock = threading.Lock()
        if directory:
            os.makedirs(directory, exist_ok=True)

    def _path(self, speaker: str, text: str, lang: str) -> str:
        key = hashlib.sha1(f"{speaker}\x00{lang}\x00{text}".encode("utf-8")).hexdigest()
        return os.path.join(self.dir, key + ".wav")

    def get(self, speaker: str, text: str, lang: str) -> bytes | None:
        if not self.dir:
            return None
        path = self._path(speaker, text, lang)
        try:
            with open(path, "rb") as fh:
                data = fh.read()
            if data:
                os.utime(path, None)  # LRU: mark as recently used
                return data
        except OSError:
            return None
        return None

    def put(self, speaker: str, text: str, lang: str, audio: bytes) -> None:
        if not self.dir or not audio:
            return
        path = self._path(speaker, text, lang)
        tmp = path + ".tmp"
        try:
            with open(tmp, "wb") as fh:
                fh.write(audio)
            os.replace(tmp, path)
        except OSError:
            return
        self._evict()

    def _evict(self) -> None:
        try:
            entries = [
                (os.path.join(self.dir, n), os.path.getmtime(os.path.join(self.dir, n)),
                 os.path.getsize(os.path.join(self.dir, n)))
                for n in os.listdir(self.dir)
                if n.endswith(".wav")
            ]
        except OSError:
            return
        total = sum(e[2] for e in entries)
        if total <= self.max_bytes:
            return
        for path, _mtime, size in sorted(entries, key=lambda e: e[1]):
            try:
                os.remove(path)
                total -= size
            except OSError:
                continue
            if total <= self.max_bytes:
                break


# --------------------------------------------------------------------------
# HTTP surface
# --------------------------------------------------------------------------


class Bridge:
    def __init__(self, host: str, cache_dir: str, cache_mb: int) -> None:
        self.space = SpaceClient(host)
        self.cache = Cache(cache_dir, cache_mb)
        # The Space queues anyway, and PetPC synthesises one sentence at a time;
        # serialising here keeps the queue position honest and avoids bursting a
        # free service.
        self._synth_lock = threading.Lock()
        self.speaker = ""

    def voices(self) -> list[str]:
        return self.space.fetch_voices()


def make_handler(bridge: Bridge, token: str):
    class Handler(BaseHTTPRequestHandler):
        server_version = "PetPCOnlineBridge/1.0"

        # --- helpers ----------------------------------------------------
        def _json(self, code: int, payload: dict[str, Any]) -> None:
            body = json.dumps(payload, ensure_ascii=False).encode("utf-8")
            self.send_response(code)
            self.send_header("Content-Type", "application/json; charset=utf-8")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def _authorised(self) -> bool:
            if not token:
                return True
            got = (self.headers.get("Authorization") or "").removeprefix("Bearer ").strip()
            if got == token:
                return True
            self._json(401, {"message": "unauthorized: set the API key in PetPC's settings"})
            return False

        def log_message(self, fmt: str, *args: Any) -> None:  # noqa: A003
            # Quiet: the bridge already logs what matters, and a line per request
            # would drown it.
            return

        # --- routes -----------------------------------------------------
        def do_GET(self) -> None:  # noqa: N802
            parsed = urllib.parse.urlparse(self.path)
            params = urllib.parse.parse_qs(parsed.query)

            if parsed.path == "/health":
                self._json(200, {"ok": True, "host": bridge.space.host,
                                 "speaker": bridge.speaker})
                return

            if parsed.path == "/voices":
                try:
                    names = bridge.voices()
                except Exception as exc:  # noqa: BLE001
                    self._json(502, {"message": f"无法获取音色列表：{exc}"})
                    return
                self._json(200, {"voices": names, "count": len(names)})
                return

            if parsed.path in ("/set_gpt_weights", "/set_sovits_weights"):
                # PetPC calls both when switching character. Only the GPT one
                # carries the voice here; the SoVITS call is a no-op so that the
                # client's existing two-step switch works untouched.
                if parsed.path == "/set_gpt_weights":
                    name = (params.get("weights_path") or [""])[0]
                    if name:
                        bridge.speaker = name
                        log(f"speaker -> {name}")
                self._json(200, {"message": "success"})
                return

            self._json(404, {"message": "not found"})

        def do_POST(self) -> None:  # noqa: N802
            parsed = urllib.parse.urlparse(self.path)
            if parsed.path != "/tts":
                self._json(404, {"message": "not found"})
                return
            if not self._authorised():
                return

            length = int(self.headers.get("Content-Length") or 0)
            try:
                req = json.loads(self.rfile.read(length).decode("utf-8"))
            except Exception:  # noqa: BLE001
                self._json(400, {"message": "invalid JSON body"})
                return

            text = (req.get("text") or "").strip()
            lang = req.get("text_lang") or "zh"
            # The speaker comes from ref_audio_path for parity with GPT-SoVITS:
            # PetPC already sends the character's reference path, so the bridge
            # can read the voice out of it. An explicit /set_gpt_weights call
            # wins, so both client styles work.
            speaker = bridge.speaker or (req.get("ref_audio_path") or "").strip()

            if not text:
                self._json(400, {"message": "text is required"})
                return
            if not speaker:
                self._json(400, {
                    "message": "no voice selected: choose one, or call "
                               "/set_gpt_weights?weights_path=<voice name>"
                })
                return
            if len(text) > MAX_TEXT_CHARS:
                self._json(400, {
                    "message": f"text is {len(text)} chars; this service accepts at "
                               f"most {MAX_TEXT_CHARS} per request. Split it first."
                })
                return

            cached = bridge.cache.get(speaker, text, lang)
            if cached:
                self._send_wav(cached)
                return

            try:
                with bridge._synth_lock:
                    audio = bridge.space.synthesize(text, speaker, lang)
            except SilentFailure:
                hint = bridge.space.speaker_hint(speaker)
                self._json(400, {"message": hint or "该音色没有返回音频"})
                return
            except Exception as exc:  # noqa: BLE001
                self._json(502, {"message": str(exc)})
                return

            bridge.cache.put(speaker, text, lang, audio)
            self._send_wav(audio)

        def _send_wav(self, audio: bytes) -> None:
            self.send_response(200)
            self.send_header("Content-Type", "audio/wav")
            self.send_header("Content-Length", str(len(audio)))
            self.end_headers()
            self.wfile.write(audio)

    return Handler


# --------------------------------------------------------------------------
# entry point
# --------------------------------------------------------------------------


def main() -> int:
    ap = argparse.ArgumentParser(description="Online voice bridge for PetPC")
    ap.add_argument("--host", default=DEFAULT_HOST,
                    help="the HuggingFace Space host to serve from")
    ap.add_argument("--bind", default="127.0.0.1")
    ap.add_argument("--port", type=int, default=9881,
                    help="default 9881, so it can coexist with a local api_v2.py on 9880")
    ap.add_argument("--cache-dir", default=os.path.join(os.path.expanduser("~"), ".petpc-online-tts"),
                    help="where synthesised WAVs are cached ('' disables)")
    ap.add_argument("--cache-mb", type=int, default=300)
    ap.add_argument("--token", default="",
                    help="optional shared secret; PetPC sends it as the API key")
    ap.add_argument("--list-voices", action="store_true",
                    help="print the available voices and exit")
    ap.add_argument("--speaker", default="", help="voice to use from the start")
    args = ap.parse_args()

    if args.list_voices:
        client = SpaceClient(args.host)
        names = client.fetch_voices()
        print(f"{len(names)} voices on {args.host}:")
        for i, n in enumerate(names):
            print(f"  [{i:3d}] {n}")
        return 0

    bridge = Bridge(args.host, args.cache_dir, args.cache_mb)
    bridge.speaker = args.speaker

    log(f"bridge  http://{args.bind}:{args.port}  ->  {args.host}")
    log(f"cache   {args.cache_dir or '(disabled)'} ({args.cache_mb} MB)")
    log("checking the upstream service...")
    if bridge.space.health():
        try:
            log(f"upstream is up; {len(bridge.voices())} voices available")
        except Exception as exc:  # noqa: BLE001
            log(f"upstream answered but the voice list failed: {exc}")
    else:
        log("WARNING: the upstream service did not answer; requests will fail until it does")
    log("point PetPC at this address:  http://%s:%d" % (args.bind, args.port))

    httpd = ThreadingHTTPServer((args.bind, args.port), make_handler(bridge, args.token))
    httpd.daemon_threads = True
    try:
        httpd.serve_forever()
    except KeyboardInterrupt:
        log("stopping")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

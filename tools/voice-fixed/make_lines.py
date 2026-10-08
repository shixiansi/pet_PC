"""Generate every fixed line the pet says, once, and save them for embedding.

The real-time synthesis path is being retired: it was slow (11s for a 54-character
reply), heavy (3.3GB resident for the local model), and unstable (the upstream
service documents its own output as random). None of that matters for a FIXED line,
because a fixed line can be synthesised once and replayed forever - zero latency,
zero memory, zero dependency.

This writes one WAV per line into internal/voice/lines, which go:embed bakes into
the binary. Run it again only when a line's TEXT changes.

Numbers are deliberately absent from the reminder lines. A fixed clip cannot have a
count spliced into it, and pre-rendering 1..99 would be hundreds of files for
information the bubble already shows. "有待办逾期了" carries the part that matters
when you are not looking at the screen.
"""
import asyncio
import base64
import json
import os
import struct
import sys
import time
import wave

import websockets  # type: ignore

sys.stdout.reconfigure(encoding="utf-8")

HOST = "zomehwh-vits-uma-genshin-honkai.hf.space"
OUT = r"F:\pet_pc\internal\voice\lines"

# key -> text. The key is what Go asks for; the text is what gets synthesised.
LINES = {
    "focus_end":    "专注时间结束了，起来活动一下吧。",
    "break_end":    "休息结束了，我们继续吧。",
    "reminder_due": "有些事情到期了，记得看一下。",
    "reminder_overdue": "有事情已经逾期了，记得处理一下。",
    "reminder_none": "现在没有待办，好好休息吧。",
}


async def synth(text: str, speaker: str, tag: str) -> bytes | None:
    data = [text, "中文", speaker, 0.6, 0.668, 1.2]
    try:
        async with websockets.connect(
            f"wss://{HOST}/queue/join", open_timeout=30, max_size=None
        ) as ws:
            await asyncio.wait_for(ws.recv(), timeout=15)
            job = {"fn_index": 0, "data": data, "session_hash": f"fix-{tag}-{time.time_ns()}"}
            await ws.send(json.dumps(job))
            asked = False
            while True:
                try:
                    obj = json.loads(await asyncio.wait_for(ws.recv(), timeout=200))
                except (asyncio.TimeoutError, websockets.exceptions.ConnectionClosed):
                    return None
                m = obj.get("msg")
                if m == "send_data" and not asked:
                    await ws.send(json.dumps(job))
                    asked = True
                    continue
                if m == "process_completed":
                    d = obj.get("output", {}).get("data") or []
                    if len(d) >= 2 and isinstance(d[1], str) and d[1].startswith("data:audio"):
                        return base64.b64decode(d[1].split(",", 1)[1])
                    return None
    except Exception as exc:
        print(f"    {type(exc).__name__}: {exc}")
        return None


def duration(raw: bytes) -> float:
    with open("_d.wav", "wb") as fh:
        fh.write(raw)
    with wave.open("_d.wav", "rb") as w:
        sec = w.getnframes() / w.getframerate()
        rate, ch, width = w.getframerate(), w.getnchannels(), w.getsampwidth()
    os.remove("_d.wav")
    print(f"      {sec:5.2f}s  {rate}Hz  {ch}ch  {width * 8}bit  {len(raw)} bytes")
    return sec


async def main() -> None:
    speaker = sys.argv[1] if len(sys.argv) > 1 else "可莉"
    os.makedirs(OUT, exist_ok=True)
    print(f"角色: {speaker}")
    print(f"输出: {OUT}\n")

    total = 0.0
    for key, text in LINES.items():
        print(f"  {key}: {text}")
        raw = await synth(text, speaker, key)
        if not raw:
            print("      FAILED")
            continue
        path = os.path.join(OUT, key + ".wav")
        with open(path, "wb") as fh:
            fh.write(raw)
        total += duration(raw)

    print(f"\n共 {len(LINES)} 条，合计 {total:.2f}s")
    size = sum(os.path.getsize(os.path.join(OUT, f)) for f in os.listdir(OUT))
    print(f"磁盘占用 {size / 1024:.0f} KB")


asyncio.run(main())

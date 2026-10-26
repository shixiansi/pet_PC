"""Does one long request lose content compared with splitting it in two?

The user reports that long sentences are sometimes read incompletely. The service
caps input at 100 characters (measured: 110 is refused outright), and its own page
warns the output is stochastic. This measures the one thing that decides the fix:
whether the SAME words produce more audio as two short requests than as one long
one. If they do, a long request is dropping content and splitting is the cure.

Content is fixed; only the request boundaries change.
"""
import asyncio
import base64
import json
import os
import time

import websockets  # type: ignore

HOST = "zomehwh-vits-uma-genshin-honkai.hf.space"
SPEAKER = "可莉"

# Natural Chinese with interior commas, so a split has a real clause boundary to
# land on rather than an arbitrary cut.
FULL = (
    "今天是十月二十四日星期五，你一共有五件待办，其中两件今天到期。"
    "上午的复习计划还没有开始，下午三点有一个会议，晚上还要整理这周的记录。"
)


def split_at_comma(text: str) -> list[str]:
    """Split on interior commas, then merge halves that are still long."""
    parts, cur = [], ""
    for ch in text:
        cur += ch
        if ch in "，。；":
            parts.append(cur)
            cur = ""
    if cur:
        parts.append(cur)
    return [p for p in parts if p.strip()]


async def synth(text: str, tag: str) -> tuple[float, float]:
    """Returns (audio seconds, generation seconds)."""
    data = [text, "中文", SPEAKER, 0.6, 0.668, 1.2]
    async with websockets.connect(f"wss://{HOST}/queue/join", open_timeout=30, max_size=None) as ws:
        await asyncio.wait_for(ws.recv(), timeout=15)
        job = {"fn_index": 0, "data": data, "session_hash": f"split-{tag}-{int(time.time()*1000)}"}
        await ws.send(json.dumps(job))
        asked = False
        t0 = time.time()
        while True:
            try:
                obj = json.loads(await asyncio.wait_for(ws.recv(), timeout=240))
            except (asyncio.TimeoutError, websockets.exceptions.ConnectionClosed):
                return 0.0, time.time() - t0
            m = obj.get("msg")
            if m == "send_data" and not asked:
                await ws.send(json.dumps(job))
                asked = True
                continue
            if m == "process_completed":
                d = obj.get("output", {}).get("data") or []
                gen = time.time() - t0
                if len(d) >= 2 and isinstance(d[1], str) and d[1].startswith("data:audio"):
                    raw = base64.b64decode(d[1].split(",", 1)[1])
                    br = int.from_bytes(raw[28:32], "little")
                    return (len(raw) - 44) / br, gen
                return 0.0, gen


async def main() -> None:
    n = len(FULL)
    parts = split_at_comma(FULL)
    print(f"text: {n} chars, splits into {len(parts)} part(s): {[len(p) for p in parts]}")

    # A: the whole thing in one request.
    sec_a, gen_a = await synth(FULL, "whole")
    print(f"  ONE request   : {sec_a:6.2f}s audio, {gen_a:5.2f}s to generate, "
          f"{sec_a / n * 100:.3f} s per 100 chars")

    # B: the same words as separate requests, summed.
    total = 0.0
    gentotal = 0.0
    for i, p in enumerate(parts):
        s, g = await synth(p, f"part{i}")
        print(f"    part {i} ({len(p):2d} chars): {s:5.2f}s audio, {g:5.2f}s to generate")
        total += s
        gentotal += g
    print(f"  SPLIT requests: {total:6.2f}s audio, {gentotal:5.2f}s to generate, "
          f"{total / n * 100:.3f} s per 100 chars")

    print()
    if total > sec_a * 1.05:
        pct = (total - sec_a) / sec_a * 100
        print(f"VERDICT: splitting produces {pct:.0f}% more audio for the same words")
        print("         -> a single long request drops content; split it.")
    else:
        print("VERDICT: no meaningful difference; the whole request covers the text.")
        print("         (a short split may still help for other reasons, but not this.)")


asyncio.run(main())

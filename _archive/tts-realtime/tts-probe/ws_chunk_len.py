"""How long may one request be before content starts going missing?

The user reports long sentences are sometimes read incompletely, and an earlier
measurement agreed: the same 66 characters produced 6% more audio as six short
requests than as one long one. This pins down WHERE that loss starts, which is what
decides the split threshold - guessing it would either split far too eagerly (more
requests, more latency) or not at all.

Each case sends the same words, only grouped differently.
"""
import asyncio
import base64
import json
import time

import websockets  # type: ignore

HOST = "zomehwh-vits-uma-genshin-honkai.hf.space"
SPEAKER = "可莉"

# A 60-character Chinese passage with clean clause boundaries, so every grouping
# cuts at a natural place rather than mid-phrase.
CLAUSES = [
    "今天是十月二十四日",
    "你一共有五件待办要处理",
    "其中有两件事情今天就会到期",
    "上午的复习计划还没有开始",
]


async def synth(text: str, tag: str) -> float:
    """Returns the audio duration in seconds, or 0 on failure."""
    data = [text, "中文", SPEAKER, 0.6, 0.668, 1.2]
    async with websockets.connect(f"wss://{HOST}/queue/join", open_timeout=30, max_size=None) as ws:
        await asyncio.wait_for(ws.recv(), timeout=15)
        job = {"fn_index": 0, "data": data,
               "session_hash": f"len-{tag}-{int(time.time() * 1000)}"}
        await ws.send(json.dumps(job))
        asked = False
        while True:
            try:
                obj = json.loads(await asyncio.wait_for(ws.recv(), timeout=240))
            except (asyncio.TimeoutError, websockets.exceptions.ConnectionClosed):
                return 0.0
            m = obj.get("msg")
            if m == "send_data" and not asked:
                await ws.send(json.dumps(job))
                asked = True
                continue
            if m == "process_completed":
                d = obj.get("output", {}).get("data") or []
                if len(d) >= 2 and isinstance(d[1], str) and d[1].startswith("data:audio"):
                    raw = base64.b64decode(d[1].split(",", 1)[1])
                    br = int.from_bytes(raw[28:32], "little")
                    return (len(raw) - 44) / br
                return 0.0


async def main() -> None:
    full = "，".join(CLAUSES) + "。"
    n = len(full)
    print(f"passage: {n} chars, {len(CLAUSES)} clauses\n")

    # Reference: every clause on its own. Each is short, so this is the most
    # audio the words can possibly produce.
    ref = 0.0
    for i, c in enumerate(CLAUSES):
        s = await synth(c, f"c{i}")
        print(f"  clause {i} ({len(c):2d} chars) -> {s:5.2f}s")
        ref += s
    per100 = ref / n * 100
    print(f"\n  reference (all {len(CLAUSES)} separate): {ref:6.2f}s "
          f"= {per100:.2f}s per 100 chars\n")

    # Now the same words grouped into progressively longer single requests.
    for k in (2, 3, 4):
        size = max(1, len(CLAUSES) // k)
        groups = ["，".join(CLAUSES[i:i + size]) for i in range(0, len(CLAUSES), size)]
        total = 0.0
        sizes = []
        for i, g in enumerate(groups):
            s = await synth(g, f"g{k}-{i}")
            total += s
            sizes.append(len(g))
        ratio = total / ref * 100 if ref else 0
        print(f"  {k} request(s), lengths {sizes}: {total:6.2f}s "
              f"= {ratio:5.1f}% of the reference audio")

    print("\nA ratio well under 100% means that grouping lost content.")


asyncio.run(main())

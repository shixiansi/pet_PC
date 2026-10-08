"""Is long-request content loss REAL, or is it just the service's randomness?

This matters because the previous probe compared each grouping ONCE and reported
"6% more audio when split" / "89.3% of reference". But the service's own page warns
结果有随机性 (the output is stochastic, "generate several times and take the best"),
and the same four-clause grouping measured 100% in one run and 93.1% in another. A
difference that changes between runs is noise, not truncation, and building a fix on
it would be building on nothing.

So this repeats each grouping and compares the DISTRIBUTIONS. The question it
answers: does a long request systematically produce less audio per character than
short ones, across repeats?
"""
import asyncio
import base64
import json
import statistics
import time

import websockets  # type: ignore

HOST = "zomehwh-vits-uma-genshin-honkai.hf.space"
SPEAKER = "可莉"
REPEATS = 3

# 48 characters in four clauses. Every grouping below uses exactly these words.
CLAUSES = [
    "今天是十月二十四日",
    "你一共有五件待办",
    "其中两件今天到期",
    "上午的复习还没开始",
]
FULL = "，".join(CLAUSES) + "。"


async def synth(text: str, tag: str) -> float:
    data = [text, "中文", SPEAKER, 0.6, 0.668, 1.2]
    async with websockets.connect(f"wss://{HOST}/queue/join", open_timeout=30, max_size=None) as ws:
        await asyncio.wait_for(ws.recv(), timeout=15)
        job = {"fn_index": 0, "data": data,
               "session_hash": f"noise-{tag}-{time.time_ns()}"}
        await ws.send(json.dumps(job))
        asked = False
        while True:
            try:
                obj = json.loads(await asyncio.wait_for(ws.recv(), timeout=240))
            except (asyncio.TimeoutError, websockets.exceptions.ConnectionClosed):
                return -1.0
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
                return -1.0


async def group_seconds(grouping: list[str], label: str) -> list[float]:
    """Total audio seconds for one whole passage, repeated."""
    out = []
    for r in range(REPEATS):
        total = 0.0
        for i, g in enumerate(grouping):
            s = await synth(g, f"{label}-{r}-{i}")
            if s < 0:
                total = -1.0
                break
            total += s
        out.append(total)
        print(f"    {label} run {r + 1}: {total:.2f}s")
    return out


async def main() -> None:
    n = len(FULL)
    print(f"passage: {n} chars, {REPEATS} repeats per grouping\n")

    one = await group_seconds([FULL], "1x48")
    four = await group_seconds(CLAUSES, "4x12")
    two = await group_seconds(
        ["，".join(CLAUSES[:2]), "，".join(CLAUSES[2:]) + "。"], "2x24")

    def show(name: str, xs: list[float]) -> None:
        good = [x for x in xs if x > 0]
        if not good:
            print(f"  {name}: all runs failed")
            return
        m = statistics.mean(good)
        print(f"  {name:6s}: mean {m:6.2f}s  range {min(good):.2f}-{max(good):.2f}s  "
              f"({m / n * 100:.2f}s per 100 chars)")

    print()
    show("1x48", one)
    show("2x24", two)
    show("4x12", four)

    good4 = [x for x in four if x > 0]
    good1 = [x for x in one if x > 0]
    if good4 and good1:
        ref = statistics.mean(good4)
        long_mean = statistics.mean(good1)
        spread4 = max(good4) - min(good4)
        spread1 = max(good1) - min(good1)
        diff = (ref - long_mean) / ref * 100
        print(f"\n  one long request vs the four-clause reference: {diff:+.1f}%")
        print(f"  run-to-run spread: 1x48 = {spread1:.2f}s, 4x12 = {spread4:.2f}s")
        if abs(diff) <= max(spread4, 0.01) / ref * 100:
            print("\n  VERDICT: the difference is within the service's own run-to-run")
            print("           variation -> NOT systematic truncation.")
        else:
            print("\n  VERDICT: long requests systematically produce less audio.")


asyncio.run(main())

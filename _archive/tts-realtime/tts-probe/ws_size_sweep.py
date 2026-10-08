"""At what chunk size does the service start losing content?

The user reports that beyond about 15 characters the voice is sometimes cut off, so
the splitter should cut at commas and periods to stay under that. This measures
whether that is real, and where the boundary is - guessing it would either split
far too eagerly (more requests, more latency) or not enough (still truncating).

Method: one fixed 55-character passage whose commas fall every 7-8 characters. The
WORDS are identical in every case; only the request boundaries change. Audio seconds
per character is the signal: if a grouping loses content, its audio is shorter per
character than a finer grouping's. Each grouping runs twice, because the service's
own output is stochastic and a single sample cannot separate signal from noise.
"""
import asyncio
import base64
import json
import statistics
import time

import websockets  # type: ignore

HOST = "zomehwh-vits-uma-genshin-honkai.hf.space"
SPEAKER = "可莉"
REPEATS = 2

CLAUSES = [
    "今天天气不错，",
    "我们去公园散步，",
    "顺便买点水果，",
    "晚上回来做饭。",
    "明天要早起，",
    "记得定好闹钟，",
    "别忘了带伞，",
    "路上注意安全。",
]
FULL = "".join(CLAUSES)


def group(clauses: list[str], per: int) -> list[str]:
    """Joins clauses into requests of `per` clauses each."""
    out = []
    for i in range(0, len(clauses), per):
        out.append("".join(clauses[i:i + per]))
    return out


async def synth(text: str, tag: str) -> float:
    data = [text, "中文", SPEAKER, 0.6, 0.668, 1.2]
    try:
        async with websockets.connect(f"wss://{HOST}/queue/join", open_timeout=30, max_size=None) as ws:
            await asyncio.wait_for(ws.recv(), timeout=15)
            job = {"fn_index": 0, "data": data, "session_hash": f"sz-{tag}-{time.time_ns()}"}
            await ws.send(json.dumps(job))
            asked = False
            while True:
                try:
                    obj = json.loads(await asyncio.wait_for(ws.recv(), timeout=200))
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
    except Exception:
        return -1.0


async def main() -> None:
    n = len(FULL)
    print(f"passage: {n} chars, {len(CLAUSES)} clauses, {REPEATS} repeats each\n")

    results: dict[str, list[float]] = {}
    # Fewest requests to most: the top row is the "send it whole" case the user is
    # complaining about.
    plans = [
        ("1 request  (~55 chars)", group(CLAUSES, 8)),
        ("2 requests (~28 chars)", group(CLAUSES, 4)),
        ("4 requests (~14 chars)", group(CLAUSES, 2)),
        ("8 requests (~7 chars)", group(CLAUSES, 1)),
    ]

    for label, chunks in plans:
        sizes = [len(c) for c in chunks]
        runs = []
        for r in range(REPEATS):
            total = 0.0
            for i, c in enumerate(chunks):
                s = await synth(c, f"{len(chunks)}-{r}-{i}")
                if s < 0:
                    total = -1.0
                    break
                total += s
            runs.append(total)
            print(f"  {label:24s} sizes={sizes} run{r + 1}={total:.2f}s")
        results[label] = runs

    print("\n  --- audio seconds per 100 characters (higher = more content) ---")
    base = None
    for label, _ in plans:
        good = [x for x in results[label] if x > 0]
        if not good:
            print(f"  {label:24s} all runs failed")
            continue
        m = statistics.mean(good)
        per100 = m / n * 100
        if base is None:
            base = per100
        delta = (per100 - base) / base * 100
        spread = max(good) - min(good) if len(good) > 1 else 0.0
        print(f"  {label:24s} {m:6.2f}s  = {per100:6.2f} s/100ch  "
              f"({delta:+5.1f}% vs whole, spread {spread:.2f}s)")

    print("\n  A rising s/100ch as requests get shorter means long requests lose content.")
    print("  The comparison only counts if the rise exceeds the run-to-run spread.")


asyncio.run(main())

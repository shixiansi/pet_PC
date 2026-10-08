"""Does a larger cap actually produce smoother speech, or just fewer requests?

The gap measurement ruled out silence (0.07s per seam) and the cut-quality dump
showed the breaks DO fall at commas - so the "chopped up" impression is most likely
simply the number of separate utterances. Each chunk is synthesised independently,
so it gets its own pitch contour and its own final lengthening; splicing three of
those together sounds like three sentences even when the words run on.

This renders the SAME reply at two caps and measures the pitch contour across the
seam, which is the thing the ear is reacting to. A large jump at a seam is an
audible reset.
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
SPEAKER = "安柏"
OUT = r"D:\GPT-SoVITS\试听"

REPLY = "今天是十月二十四日星期五，你一共有五件待办，其中两件今天到期。"

AT_15 = ["今天是十月二十四日星期五，", "你一共有五件待办，", "其中两件今天到期。"]
AT_25 = ["今天是十月二十四日星期五，你一共有五件待办，", "其中两件今天到期。"]


async def synth(text: str, tag: str) -> bytes | None:
    data = [text, "中文", SPEAKER, 0.6, 0.668, 1.2]
    try:
        async with websockets.connect(
            f"wss://{HOST}/queue/join", open_timeout=30, max_size=None
        ) as ws:
            await asyncio.wait_for(ws.recv(), timeout=15)
            job = {"fn_index": 0, "data": data, "session_hash": f"pitch-{tag}-{time.time_ns()}"}
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
    except Exception:
        return None


def pcm_of(raw: bytes) -> tuple[list[int], int]:
    with open("_p.wav", "wb") as fh:
        fh.write(raw)
    with wave.open("_p.wav", "rb") as w:
        rate = w.getframerate()
        ch = w.getnchannels()
        frames = w.readframes(w.getnframes())
    os.remove("_p.wav")
    samples = struct.unpack("<" + "h" * (len(frames) // 2), frames)
    if ch == 2:
        samples = samples[0::2]
    return list(samples), rate


def rms(samples: list[int], start: int, n: int, rate: int) -> float:
    """RMS over a window, used as a crude loudness/energy proxy."""
    seg = samples[start:start + n]
    if not seg:
        return 0.0
    return (sum(s * s for s in seg) / len(seg)) ** 0.5


def analyse(parts: list[list[int]], rate: int, label: str) -> None:
    """Reports the energy dip at each seam - the audible 'reset'."""
    print(f"  {label}:")
    seam_dips = []
    for i in range(len(parts) - 1):
        prev, nxt = parts[i], parts[i + 1]
        win = int(rate * 0.04)  # 40ms
        before = rms(prev, max(0, len(prev) - win), win, rate)
        after = rms(nxt, 0, win, rate)
        # The quietest 40ms window in the 120ms either side of the seam: that is
        # where the ear hears the break.
        around_prev = min(
            rms(prev, max(0, len(prev) - int(rate * 0.12) + k), win, rate)
            for k in range(0, int(rate * 0.08), int(rate * 0.01))
        ) if len(prev) > int(rate * 0.2) else before
        around_next = min(
            rms(nxt, k, win, rate)
            for k in range(0, int(rate * 0.08), int(rate * 0.01))
        ) if len(nxt) > int(rate * 0.2) else after
        seam = min(around_prev, around_next)
        peak = max(before, after, 1.0)
        ratio = seam / peak
        seam_dips.append(ratio)
        print(f"    接缝 {i + 1}: 前后能量 {before:7.0f} -> {after:7.0f}, "
              f"最低谷 {seam:7.0f} (占 {ratio * 100:4.0f}%)")
    if seam_dips:
        worst = min(seam_dips)
        print(f"    最深的能量谷: {worst * 100:.0f}%  (越低越像断开)")
        print(f"    接缝数: {len(seam_dips)}")


async def main() -> None:
    rate = 0
    print(f"回复: {REPLY}\n")

    for label, chunks, fname in (
        ("上限 15 字", AT_15, "pitch_15.wav"),
        ("上限 25 字", AT_25, "pitch_25.wav"),
    ):
        print(f"{label} -> {len(chunks)} 段")
        parts = []
        for i, c in enumerate(chunks):
            raw = await synth(c, f"{len(chunks)}-{i}")
            if not raw:
                print(f"    [{i}] 失败")
                continue
            samples, rate = pcm_of(raw)
            parts.append(samples)
        if len(parts) < 2:
            print("    段数不足，无法比较接缝\n")
            continue
        analyse(parts, rate, label)

        # Also write the concatenation so it can be listened to directly.
        joined = [s for p in parts for s in p]
        path = os.path.join(OUT, fname)
        with wave.open(path, "wb") as w:
            w.setnchannels(1)
            w.setsampwidth(2)
            w.setframerate(rate)
            w.writeframes(struct.pack("<" + "h" * len(joined), *joined))
        print(f"    已写出: {path}")
        print()

    print("能量谷越深、接缝越多，听感越像被切断。")


asyncio.run(main())

"""How much silence does chunking add, and does it explain the "broken" sound?

The user reports the result sounds like a sentence that has been chopped up. The
suspect is concrete: the reply is played as N SEPARATE WAV files, each of which
carries its own leading and trailing silence, plus a disk write and a PlaySound
call between them. Those add up to an audible break every chunk.

This measures, on the real service:
  * how much leading/trailing silence each returned clip has
  * how much silence the chunking therefore injects into one reply
  * what the same reply looks like as a SINGLE concatenated file

Silence is found by scanning 16-bit samples for the first/last one above a
threshold. The threshold is deliberately low (a few hundred of 32767) so quiet
speech is not mistaken for silence.
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

# The chunks PetPC would actually send for this reply at a 15-rune cap.
REPLY = "今天是十月二十四日星期五，你一共有五件待办，其中两件今天到期。"
CHUNKS = [
    "今天是十月二十四日星期五，",
    "你一共有五件待办，",
    "其中两件今天到期。",
]


async def synth(text: str, tag: str) -> bytes | None:
    data = [text, "中文", SPEAKER, 0.6, 0.668, 1.2]
    try:
        async with websockets.connect(
            f"wss://{HOST}/queue/join", open_timeout=30, max_size=None
        ) as ws:
            await asyncio.wait_for(ws.recv(), timeout=15)
            job = {"fn_index": 0, "data": data, "session_hash": f"gap-{tag}-{time.time_ns()}"}
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


def analyse(raw: bytes, label: str) -> tuple[float, float, float, int, int, bytes]:
    """Returns (total_s, lead_silence_s, tail_silence_s, rate, width, pcm)."""
    with open("_tmp.wav", "wb") as fh:
        fh.write(raw)
    with wave.open("_tmp.wav", "rb") as w:
        rate = w.getframerate()
        width = w.getsampwidth()
        ch = w.getnchannels()
        frames = w.readframes(w.getnframes())
    os.remove("_tmp.wav")

    if width != 2:
        return len(frames) / (rate * ch * width), 0.0, 0.0, rate, width, frames

    samples = struct.unpack("<" + "h" * (len(frames) // 2), frames)
    thresh = 300
    first = 0
    for i, s in enumerate(samples):
        if abs(s) > thresh:
            first = i
            break
    last = len(samples) - 1
    for i in range(len(samples) - 1, -1, -1):
        if abs(samples[i]) > thresh:
            last = i
            break

    per = rate * ch
    total = len(samples) / per
    lead = first / per
    tail = (len(samples) - 1 - last) / per
    print(f"  {label:24s} 总 {total:5.2f}s  开头静音 {lead:5.2f}s  结尾静音 {tail:5.2f}s")
    return total, lead, tail, rate, width, frames


async def main() -> None:
    os.makedirs(OUT, exist_ok=True)
    print(f"回复 {len(REPLY)} 字，按 15 字上限切成 {len(CHUNKS)} 段\n")

    print("各段单独合成：")
    parts = []
    for i, c in enumerate(CHUNKS):
        raw = await synth(c, f"c{i}")
        if not raw:
            print(f"  [{i}] 合成失败")
            continue
        parts.append(analyse(raw, f"[{i}] {c[:10]}…"))

    if not parts:
        print("没有可用样本")
        return

    total_audio = sum(p[0] for p in parts)
    total_lead = sum(p[1] for p in parts)
    total_tail = sum(p[2] for p in parts)
    # Between two clips, the previous one's tail and the next one's lead are both
    # silence, so the audible break is their SUM.
    interior_gaps = total_tail + total_lead - (parts[0][1] + parts[-1][2])

    print()
    print(f"  纯语音总时长        {total_audio:5.2f}s")
    print(f"  开头静音合计        {total_lead:5.2f}s")
    print(f"  结尾静音合计        {total_tail:5.2f}s")
    print(f"  段与段之间的静音    {interior_gaps:5.2f}s  <- 这就是持续断开感的来源")
    print(f"  共 {len(parts) - 1} 个接缝，平均每处 {interior_gaps / max(1, len(parts) - 1):.2f}s")

    # Build the single concatenated file the fix would play, and report its size.
    rate, width, ch = parts[0][3], parts[0][4], 1
    combined = b"".join(p[5] for p in parts)
    path = os.path.join(OUT, "gap_concatenated.wav")
    with wave.open(path, "wb") as w:
        w.setnchannels(ch)
        w.setsampwidth(width)
        w.setframerate(rate)
        w.writeframes(combined)
    print()
    print(f"  拼成单个文件: {len(combined) / (rate * ch * width):.2f}s -> {path}")
    print("  （播放单个文件时，接缝处仍然是各段自带的静音，但少了写盘与调用开销）")

    print()
    if interior_gaps > 0.3:
        print("结论：分段在接缝处注入了可观的静音 —— 这就是听起来'断掉'的原因。")
    else:
        print("结论：静音不足以解释'断掉'，需要看别的原因。")


asyncio.run(main())

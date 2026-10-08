"""Does feeding the model a HALF-SENTENCE make it sound cut off?

Every previous measurement looked at length and at silence between clips. Neither
explained the "broken sentence" the user hears. This tests a different hypothesis
about the CAUSE: the text is being cut at commas, so what is sent to the model is
an INCOMPLETE sentence ("今天是十月二十四日星期五，"). A TTS model given a fragment
cannot produce a natural ending - it has no sentence-final prosody - so each clip
sounds like it was interrupted, no matter how cleanly the clips are joined.

The test renders the same words twice, changing ONLY where the request boundaries
fall:
    A) at commas      -> what the current 15-rune cap produces
    B) at full stops  -> whole sentences

and measures what the model does at the END of each request: a complete sentence
should trail off (energy decaying over a longer tail), a fragment should stop dead.
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

REPLY = "今天是十月二十四日星期五，你一共有五件待办，其中两件今天到期。上午的复习计划还没有开始，下午三点有一个会议。"

# A: cut at commas to obey a 15-rune cap - the current behaviour.
BY_COMMA = [
    "今天是十月二十四日星期五，",
    "你一共有五件待办，",
    "其中两件今天到期。",
    "上午的复习计划还没有开始，",
    "下午三点有一个会议。",
]
# B: whole sentences, however long they are.
BY_SENTENCE = [
    "今天是十月二十四日星期五，你一共有五件待办，其中两件今天到期。",
    "上午的复习计划还没有开始，下午三点有一个会议。",
]


async def synth(text: str, tag: str) -> bytes | None:
    data = [text, "中文", SPEAKER, 0.6, 0.668, 1.2]
    try:
        async with websockets.connect(
            f"wss://{HOST}/queue/join", open_timeout=30, max_size=None
        ) as ws:
            await asyncio.wait_for(ws.recv(), timeout=15)
            job = {"fn_index": 0, "data": data, "session_hash": f"frag-{tag}-{time.time_ns()}"}
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


def read_pcm(raw: bytes) -> tuple[list[int], int]:
    with open("_f.wav", "wb") as fh:
        fh.write(raw)
    with wave.open("_f.wav", "rb") as w:
        rate, ch = w.getframerate(), w.getnchannels()
        frames = w.readframes(w.getnframes())
    os.remove("_f.wav")
    s = struct.unpack("<" + "h" * (len(frames) // 2), frames)
    return (list(s[0::2]) if ch == 2 else list(s)), rate


def tail_profile(samples: list[int], rate: int) -> str:
    """How the last 300ms decays: a natural ending trails off, a cut stops dead."""
    n = int(rate * 0.30)
    if len(samples) < n:
        n = len(samples)
    seg = samples[-n:]
    step = max(1, n // 6)
    parts = []
    for k in range(0, n, step):
        chunk = seg[k:k + step]
        if not chunk:
            continue
        rms = (sum(x * x for x in chunk) / len(chunk)) ** 0.5
        parts.append(rms)
    return " ".join(f"{p:5.0f}" for p in parts)


def trailing_silence(samples: list[int], rate: int, thresh: int = 300) -> float:
    last = 0
    for i in range(len(samples) - 1, -1, -1):
        if abs(samples[i]) > thresh:
            last = i
            break
    return (len(samples) - 1 - last) / rate


async def render(chunks: list[str], label: str, fname: str) -> tuple[float, list[float]]:
    print(f"--- {label}：{len(chunks)} 段 ---")
    parts: list[list[int]] = []
    rate = 0
    tails = []
    for i, c in enumerate(chunks):
        raw = await synth(c, f"{label}-{i}")
        if not raw:
            print(f"    [{i}] 失败")
            continue
        s, rate = read_pcm(raw)
        parts.append(s)
        ts = trailing_silence(s, rate)
        tails.append(ts)
        print(f"    [{i}] {len(c):2d}字  {len(s) / rate:5.2f}s  尾部静音 {ts:.3f}s  "
              f"末300ms能量: {tail_profile(s, rate)}")
        print(f"         「{c}」")
    if not parts:
        return 0.0, tails
    joined = [x for p in parts for x in p]
    with wave.open(os.path.join(OUT, fname), "wb") as w:
        w.setnchannels(1)
        w.setsampwidth(2)
        w.setframerate(rate)
        w.writeframes(struct.pack("<" + "h" * len(joined), *joined))
    total = len(joined) / rate
    print(f"    合计 {total:.2f}s -> {os.path.join(OUT, fname)}\n")
    return total, tails


async def main() -> None:
    print(f"回复 {len(REPLY)} 字\n")
    print("=== A：按逗号切（当前 15 字上限的做法）===")
    a_total, a_tails = await render(BY_COMMA, "按逗号", "frag_comma.wav")

    print("=== B：按整句切 ===")
    b_total, b_tails = await render(BY_SENTENCE, "按整句", "frag_sentence.wav")

    print("=== 对比 ===")
    print(f"  按逗号: {a_total:5.2f}s，{len(BY_COMMA)} 段，"
          f"平均尾部静音 {sum(a_tails) / len(a_tails):.3f}s")
    print(f"  按整句: {b_total:5.2f}s，{len(BY_SENTENCE)} 段，"
          f"平均尾部静音 {sum(b_tails) / len(b_tails):.3f}s")
    print()
    print("  段数以逗号结尾时，每段都是不完整的句子 —— 模型给不出句末语调，")
    print("  听起来就是每句都被掐断。整句切分则每段都有自然的收尾。")


asyncio.run(main())

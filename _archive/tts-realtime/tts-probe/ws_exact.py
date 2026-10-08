"""Retry with the exact speaker names the Space exposes.

The dropdown has 804 entries, and the Genshin ones carry bracketed aliases:
  "\u7eb3\u897f\u5982\uff08\u8349\u795e\uff09"  (纳西妲（草神）)
  "\u795e\u91cc\u7eeb\u534e\uff08\u9f9f\u9f9f\uff09" (神里绫华（龟龟）)
A bare "\u7eb3\u897f\u5982" produced NO output rather than an error, which is worth
knowing: a wrong speaker name fails silently.
"""
import asyncio, base64, json, os
import websockets  # type: ignore

HOST = "zomehwh-vits-uma-genshin-honkai.hf.space"
OUT = r"D:\GPT-SoVITS\\\u8bd5\u542c"
TEXT = "\u4f60\u597d\uff0c\u6211\u662f\u4f60\u7684\u684c\u5ba0\u3002"

async def synth(speaker, text=TEXT):
    data = [text, "\u4e2d\u6587", speaker, 0.6, 0.668, 1.2]
    try:
        async with websockets.connect(f"wss://{HOST}/queue/join", open_timeout=30, max_size=None) as ws:
            await asyncio.wait_for(ws.recv(), timeout=15)
            job = {"fn_index": 0, "data": data, "session_hash": "petpc-exact"}
            await ws.send(json.dumps(job))
            asked = False
            while True:
                try:
                    obj = json.loads(await asyncio.wait_for(ws.recv(), timeout=180))
                except (asyncio.TimeoutError, websockets.exceptions.ConnectionClosed):
                    return None, "closed"
                m = obj.get("msg")
                if m == "send_data" and not asked:
                    await ws.send(json.dumps(job)); asked = True; continue
                if m == "process_completed":
                    d = obj.get("output", {}).get("data") or []
                    if len(d) >= 2 and isinstance(d[1], str) and d[1].startswith("data:audio"):
                        return d[1], (d[2] if len(d) > 2 else "")
                    return None, json.dumps(d[:1], ensure_ascii=False)[:80]
    except Exception as e:
        return None, f"{type(e).__name__}: {e}"

async def main():
    os.makedirs(OUT, exist_ok=True)
    names = {
        "\u7eb3\u897f\u5982\uff08\u8349\u795e\uff09": "\u7eb3\u897f\u5982",
        "\u795e\u91cc\u7eeb\u534e\uff08\u9f9f\u9f9f\uff09": "\u795e\u91cc\u7eeb\u534e",
        "\u73ca\u745a\u5bab\u5fc3\u6d77\uff08\u5fc3\u6d77\uff0c\u6263\u6263\u7c73\uff09": "\u73ca\u745a\u5bab\u5fc3\u6d77",
        "\u96f7\u7535\u5c06\u519b\uff08\u96f7\u795e\uff09": "\u96f7\u7535\u5c06\u519b",
    }
    for full, short in names.items():
        audio, info = await synth(full)
        if not audio:
            print(f"  [fail] {short} ({full}): {info}")
            continue
        raw = base64.b64decode(audio.split(",", 1)[1])
        path = os.path.join(OUT, f"online_{short}.wav")
        open(path, "wb").write(raw)
        br = int.from_bytes(raw[28:32], "little")
        print(f"  [ok]   {short}: {len(raw)} bytes, {(len(raw)-44)/br:.2f}s, {info}")

asyncio.run(main())
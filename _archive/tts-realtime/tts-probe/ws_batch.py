"""Synthesise several Genshin characters and save the WAVs for listening."""
import asyncio, base64, json, os, re
import websockets  # type: ignore

HOST = "zomehwh-vits-uma-genshin-honkai.hf.space"
OUT = r"D:\GPT-SoVITS\\\u8bd5\u542c"   # 试听
TEXT = "\u4f60\u597d\uff0c\u6211\u662f\u4f60\u7684\u684c\u5ba0\u3002\u73b0\u5728\u6709\u4e09\u4ef6\u4e8b\u60c5\u4eca\u5929\u5230\u671f\uff0c\u5148\u4ece\u54ea\u4e00\u4ef6\u5f00\u59cb\u5462\uff1f"

async def synth(speaker, text):
    data = [text, "\u4e2d\u6587", speaker, 0.6, 0.668, 1.2]
    try:
        async with websockets.connect(f"wss://{HOST}/queue/join", open_timeout=30, max_size=None) as ws:
            await asyncio.wait_for(ws.recv(), timeout=15)
            job = {"fn_index": 0, "data": data, "session_hash": "petpc-batch"}
            await ws.send(json.dumps(job))
            asked = False
            while True:
                try:
                    obj = json.loads(await asyncio.wait_for(ws.recv(), timeout=180))
                except (asyncio.TimeoutError, websockets.exceptions.ConnectionClosed):
                    return None, None
                m = obj.get("msg")
                if m == "send_data" and not asked:
                    await ws.send(json.dumps(job)); asked = True; continue
                if m == "process_completed":
                    d = obj.get("output", {}).get("data") or []
                    if len(d) >= 2 and isinstance(d[1], str) and d[1].startswith("data:audio"):
                        return d[1], (d[2] if len(d) > 2 else "")
                    return None, None
    except Exception as e:
        return None, f"{type(e).__name__}: {e}"

async def main():
    os.makedirs(OUT, exist_ok=True)
    speakers = ["\u53ef\u8389", "\u7eb3\u897f\u5982", "\u795e\u91cc\u7eeb\u534e", "\u5bb5\u5bab", "\u80e1\u6843"]
    for spk in speakers:
        audio, info = await synth(spk, TEXT)
        if not audio:
            print(f"  [skip] {spk}: {info}")
            continue
        raw = base64.b64decode(audio.split(",", 1)[1])
        path = os.path.join(OUT, f"online_{spk}.wav")
        with open(path, "wb") as f:
            f.write(raw)
        # duration from the WAV header
        br = int.from_bytes(raw[28:32], "little")
        sec = (len(raw) - 44) / br if br else 0
        print(f"  [ok] {spk}: {len(raw)} bytes, {sec:.2f}s, {info}")

asyncio.run(main())
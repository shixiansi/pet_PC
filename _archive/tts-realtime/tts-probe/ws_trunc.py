"""Test whether the Space truncates long text.

The user reports some characters do not read the whole sentence. The Space's own
UI says "Text (100 words limitation)", so truncation is plausible. This sends the
SAME long text to several speakers and compares the resulting duration, which is
the only way to tell "this voice is just faster" from "this voice stopped early".
"""
import asyncio, base64, json, os
import websockets  # type: ignore

HOST = "zomehwh-vits-uma-genshin-honkai.hf.space"
OUT = r"D:\GPT-SoVITS\\u8bd5\\u542c"
LONG = ("\u4eca\u5929\u662f\u5341\u6708\u4e8c\u5341\u56db\u65e5\u661f\u671f\u4e94\u3002"
        "\u4f60\u4e00\u5171\u6709\u4e94\u4ef6\u5f85\u529e\uff0c\u5176\u4e2d\u4e24\u4ef6\u4eca\u5929\u5230\u671f\u3002"
        "\u4e0a\u5348\u7684\u590d\u4e60\u8ba1\u5212\u8fd8\u6ca1\u6709\u5f00\u59cb\uff0c"
        "\u4e0b\u5348\u4e09\u70b9\u6709\u4e00\u4e2a\u4f1a\u8bae\u3002")

async def synth(speaker, text, sess):
    data = [text, "\u4e2d\u6587", speaker, 0.6, 0.668, 1.2]
    try:
        async with websockets.connect(f"wss://{HOST}/queue/join", open_timeout=30, max_size=None) as ws:
            await asyncio.wait_for(ws.recv(), timeout=15)
            job = {"fn_index": 0, "data": data, "session_hash": sess}
            await ws.send(json.dumps(job))
            asked = False
            while True:
                try:
                    obj = json.loads(await asyncio.wait_for(ws.recv(), timeout=200))
                except (asyncio.TimeoutError, websockets.exceptions.ConnectionClosed):
                    return None, "closed"
                m = obj.get("msg")
                if m == "send_data" and not asked:
                    await ws.send(json.dumps(job)); asked = True; continue
                if m == "process_completed":
                    d = obj.get("output", {}).get("data") or []
                    if len(d) >= 2 and isinstance(d[1], str) and d[1].startswith("data:audio"):
                        return d[1], (d[2] if len(d) > 2 else "")
                    return None, json.dumps(d[:1], ensure_ascii=False)[:60]
    except Exception as e:
        return None, f"{type(e).__name__}"

async def main():
    os.makedirs(OUT, exist_ok=True)
    print(f"text: {len(LONG)} chars")
    speakers = {
        "\u53ef\u8389": "klee",
        "\u80e1\u6843": "hutao",
        "\u5bb5\u5bab": "yoimiya",
        "\u795e\u91cc\u7eeb\u534e\uff08\u9f9f\u9f9f\uff09": "ayaka",
        "\u73ca\u745a\u5bab\u5fc3\u6d77\uff08\u5fc3\u6d77\uff0c\u6263\u6263\u7c73\uff09": "kokomi",
        "\u96f7\u7535\u5c06\u519b\uff08\u96f7\u795e\uff09": "raiden",
    }
    for spk, tag in speakers.items():
        audio, info = await synth(spk, LONG, f"trunc-{tag}")
        if not audio:
            print(f"  [fail] {tag}: {info}"); continue
        raw = base64.b64decode(audio.split(",", 1)[1])
        br = int.from_bytes(raw[28:32], "little")
        sec = (len(raw)-44)/br
        open(os.path.join(OUT, f"long_{tag}.wav"), "wb").write(raw)
        # ~5 chars/second is normal Chinese TTS pacing
        print(f"  [ok] {tag:<8} {sec:5.2f}s  约合 {sec*5:4.0f} 字 / 应为 {len(LONG)} 字   {info}")

asyncio.run(main())
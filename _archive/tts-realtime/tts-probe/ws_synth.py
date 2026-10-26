"""Synthesise with the CORRECT argument order.

The dependency list uses component IDs, not array positions:
  fn_index=0 inputs=[6,7,13,15,16,17]
    id 6  = Text (100 words limitation)
    id 7  = Language
    id 13 = Speaker        <-- was missing/wrong in earlier attempts
    id 15,16,17 = noise_scale, noise_scale_w, length_scale
  outputs=[22,23,24] = Output Message, Output Audio, Extra Info

Previous attempts sent ["中文","Submit",text,...], i.e. the language where the
text belongs, which is why every job "completed" with no audio.
"""
import asyncio, json, urllib.request
import websockets  # type: ignore

HOST = "zomehwh-vits-uma-genshin-honkai.hf.space"
TEXT = "\u756a\u8304\u949f\u7ed3\u675f\u4e86\uff0c\u4f11\u606f\u4e00\u4e0b\u5427\u3002"

async def synth(speaker, text, lang="\u4e2d\u6587"):
    data = [text, lang, speaker, 0.6, 0.668, 1.2]
    async with websockets.connect(f"wss://{HOST}/queue/join", open_timeout=30, max_size=None) as ws:
        await asyncio.wait_for(ws.recv(), timeout=15)
        job = {"fn_index": 0, "data": data, "session_hash": "petpc-real"}
        await ws.send(json.dumps(job))
        asked = False
        while True:
            try:
                obj = json.loads(await asyncio.wait_for(ws.recv(), timeout=180))
            except (asyncio.TimeoutError, websockets.exceptions.ConnectionClosed):
                return None
            m = obj.get("msg")
            if m == "send_data" and not asked:
                await ws.send(json.dumps(job)); asked = True; continue
            if m == "process_completed":
                return obj.get("output", {})

async def main():
    for spk in ["\u53ef\u8389", "\u7eb3\u897f\u5982"]:  # 可莉, 纳西妲
        out = await synth(spk, TEXT)
        print(f"=== speaker={spk} ===")
        if not out:
            print("  no output"); continue
        datas = out.get("data") or []
        print(f"  outputs={len(datas)}")
        for i, item in enumerate(datas):
            print(f"    [{i}] {json.dumps(item, ensure_ascii=False)[:300]}")

asyncio.run(main())
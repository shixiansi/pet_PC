"""Merges one character's TTS settings into PetPC's config.json.

Keeps every other key untouched, and writes with ensure_ascii=False so the Chinese
stays readable - PowerShell's ConvertTo-Json would rewrite it as \\uXXXX escapes,
and this project treats config.json as a file users read by hand.

Usage:
    python merge_character.py <character>      # e.g. klee
    python merge_character.py --list           # show what is available
"""
import json
import os
import pathlib
import shutil
import sys

ROOT = pathlib.Path(r"D:\GPT-SoVITS")
CFG = pathlib.Path(os.environ["APPDATA"]) / "PetPC" / "config.json"

LABELS = {
    "klee": "可莉",
    "nahida": "纳西妲",
    "ayaka": "神里绫华",
    "yoimiya": "宵宫",
}

if len(sys.argv) < 2:
    sys.exit(__doc__)

if sys.argv[1] == "--list":
    for key, label in LABELS.items():
        d = ROOT / "models" / key
        have = all((d / f).exists() for f in ("gpt.ckpt", "sovits.pth", "ref.wav"))
        print(f"  {'OK ' if have else 'MISS'} {key:<9} {label}")
    sys.exit(0)

char = sys.argv[1]
if char not in LABELS:
    sys.exit(f"unknown character {char!r}; known: {', '.join(LABELS)}")

model_dir = ROOT / "models" / char
for f in ("gpt.ckpt", "sovits.pth", "ref.wav"):
    if not (model_dir / f).exists():
        sys.exit(f"missing {model_dir / f}")

# prompt_text is deliberately empty: it is only correct if it is the clip's exact
# wording, and a guessed one makes GPT-SoVITS align the reference to the wrong text
# (measured here: it produced laughter instead of speech).
tts = {
    "ttsEnabled": True,
    "ttsBaseUrl": "http://127.0.0.1:9880",
    "ttsCharacter": LABELS[char],
    "ttsGptWeights": str(model_dir / "gpt.ckpt"),
    "ttsSovitsWeights": str(model_dir / "sovits.pth"),
    "ttsRefAudioPath": str(model_dir / "ref.wav"),
    "ttsPromptText": "",
    "ttsLang": "zh",
}

before = json.loads(CFG.read_text(encoding="utf-8"))
shutil.copy2(CFG, CFG.with_suffix(".json.before-merge"))

after = dict(before)
after.update(tts)
CFG.write_text(json.dumps(after, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")

print(f"character: {char} ({LABELS[char]})")
for k, v in tts.items():
    old = before.get(k, "<absent>")
    if old != v:
        print(f"  {k}: {old!r} -> {v!r}")
print(f"\nwrote {CFG} ({CFG.stat().st_size} bytes, {len(after)} keys)")

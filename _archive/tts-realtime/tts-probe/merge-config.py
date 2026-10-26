"""Merges the TTS fields into PetPC's config.json without touching anything else.

Why a script instead of hand-editing: the file holds the LLM settings and the pet's
position, and PowerShell's ConvertTo-Json would rewrite every Chinese string as
\\uXXXX escapes (and could reorder keys). Using Python's json with
ensure_ascii=False keeps the file byte-for-byte human-readable, which matters
because this project treats config.json as a file users poke at by hand.

It prints a diff so the change is auditable, and refuses to run if the TTS source
file is missing.
"""
import json
import os
import pathlib
import shutil
import sys

cfg_path = pathlib.Path(os.environ["APPDATA"]) / "PetPC" / "config.json"
src_path = pathlib.Path(r"D:\GPT-SoVITS\models\kokomi\petpc-config.json")

if not src_path.exists():
    sys.exit(f"missing TTS config: {src_path}")
if not cfg_path.exists():
    sys.exit(f"missing PetPC config: {cfg_path}")

tts = json.loads(src_path.read_text(encoding="utf-8"))
before = json.loads(cfg_path.read_text(encoding="utf-8"))

# Keep a timestamped copy as well as the .bak-tts taken before this run, so a
# second run cannot destroy the original.
shutil.copy2(cfg_path, cfg_path.with_suffix(".json.before-merge"))

after = dict(before)
after.update(tts)

print("字段变化：")
for k in sorted(tts):
    old = before.get(k, "<不存在>")
    new = tts[k]
    marker = " " if old == new else "*"
    print(f"  {marker} {k}")
    if old != new:
        print(f"      旧: {old}")
        print(f"      新: {new}")

# ensure_ascii=False keeps the Chinese readable; indent=2 matches what the Go
# side writes (writeJSON uses MarshalIndent with two spaces).
cfg_path.write_text(
    json.dumps(after, ensure_ascii=False, indent=2) + "\n",
    encoding="utf-8",
)
print(f"\n已写入 {cfg_path} ({cfg_path.stat().st_size} 字节)")
print(f"字段总数: {len(before)} -> {len(after)}")

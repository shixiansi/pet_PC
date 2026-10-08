"""Is the 15-rune cap cutting phrases in half, rather than at pauses?

The gap measurement ruled out silence as the cause of the "chopped up" sound
(0.07s per seam). The remaining suspect is the TEXT: at a 15-rune cap the splitter
backs up to the last comma, but a comma is not always the end of a tone group - so
a chunk can end mid-phrase and the next one starts mid-thought, which sounds broken
even with no silence at all.

This prints what the splitter actually produces for realistic replies, so the
breaks can be judged as LANGUAGE rather than as milliseconds.
"""
import sys

sys.stdout.reconfigure(encoding="utf-8")

# Mirrors internal/tts/split.go: hard terminators end a chunk outright; soft ones
# end it once the chunk has reached target; the cap backs up to the last soft
# terminator when it leaves at least half the cap.
HARD = "。！？!?…\n"
SOFT = "，、；：,;:"
CLOSERS = "」』】》）)』\"'”’"


def split(text: str, target: int, cap: int) -> list[str]:
    out = []
    buf = text
    while buf:
        runes = list(buf)
        start = 0
        while start < len(runes) and runes[start].isspace():
            start += 1
        cut = -1
        last_term = -1
        for i in range(start, len(runes)):
            r = runes[i]
            seen = i - start + 1
            if r in HARD:
                j = i + 1
                while j < len(runes) and runes[j] in CLOSERS:
                    j += 1
                cut = j
                break
            if r in SOFT:
                last_term = i + 1
                if seen >= target:
                    cut = i + 1
                    break
            if seen >= cap:
                cut = last_term if (last_term - start) >= (cap + 1) // 2 else i + 1
                break
        if cut < 0:
            out.append(buf.strip())
            break
        piece = "".join(runes[start:cut]).strip()
        buf = "".join(runes[cut:])
        if piece:
            out.append(piece)
    return out


SAMPLES = [
    "好的，我记下了。",
    "专注时间结束了，起来活动一下吧。",
    "今天是十月二十四日星期五，你一共有五件待办，其中两件今天到期。",
    "你一共有五件待办，其中两件今天到期，上午的复习计划还没有开始，下午三点有一个会议。",
    "这个功能已经做好了，你可以先去休息一下，等会儿再回来看效果。",
]

for cap in (15, 25, 40):
    print(f"===== 上限 {cap} 字 =====")
    for text in SAMPLES:
        chunks = split(text, cap, cap)
        print(f"  原文 {len(text):2d} 字: {text}")
        for i, c in enumerate(chunks):
            mark = "└" if i == len(chunks) - 1 else " "
            print(f"    {mark} [{len(c):2d}] {c}")
        print()

    # A chunk count is not the point; whether the break falls at a natural pause is.
    print()

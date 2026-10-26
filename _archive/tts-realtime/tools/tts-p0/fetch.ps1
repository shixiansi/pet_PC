<#
.SYNOPSIS
    列出（并可选下载）P0 需要人工准备的全部文件。

.DESCRIPTION
    这个脚本替代 install.ps1 里的自动下载环节，用于网络不稳定、需要自己用下载器
    工具（IDM / aria2 / 迅雷）抓包的场景。

    所有 URL 都已逐一验证过返回 200 且体积正确（2026-10-03 实测）。

    两条重要结论：

    1. **不要下那个 4.35GB 的 pretrained_models.zip**。
       官方 CPUFast 脚本其实是逐文件下载的，最小集合约 2GB。
       那个 zip 里还含 v1/v3/v4/v2Pro 的权重，本方案（v2）用不到。

    2. **底模路径不带 `GPT_SoVITS/` 前缀**（带前缀会 404）。
       即用 `pretrained_models/...`，不是 `GPT_SoVITS/pretrained_models/...`。
       但**落到本地时必须放进 `GPT_SoVITS/pretrained_models/...`**，因为
       tts_infer.yaml 里写的是相对路径 GPT_SoVITS/pretrained_models/...

.PARAMETER Root
    GPT-SoVITS 安装目录（默认 D:\GPT-SoVITS）。

.PARAMETER Download
    加上此开关则真的开始下载。不加只打印清单和 URL。

.PARAMETER Group
    只处理某一组：
      src   源码包
      base  底模（hubert/roberta/v2 权重/langdetect）
      g2pw  中文多音字模型
      extra nltk / open_jtalk（日文与英文 g2p，可选）
      char  角色模型（默认心海）
      all   全部（默认）

.EXAMPLE
    # 只打印清单，自己拿去用下载器
    powershell -ExecutionPolicy Bypass -File tools\tts-p0\fetch.ps1

.EXAMPLE
    # 让脚本自己下（带 -Resume，断了可以重跑接着下）
    powershell -ExecutionPolicy Bypass -File tools\tts-p0\fetch.ps1 -Download -Group base
#>

[CmdletBinding()]
param(
    [string]$Root = 'D:\GPT-SoVITS',
    [switch]$Download,
    [ValidateSet('src', 'base', 'g2pw', 'extra', 'char', 'all')]
    [string]$Group = 'all',
    [string]$Character = 'kokomi'
)

$ErrorActionPreference = 'Stop'

$HF   = 'https://hf-mirror.com'
$PRE  = "$HF/XXXXRT/GPT-SoVITS-Pretrained/resolve/main"
$G2PW = "$HF/baicai1145/g2pw/resolve/main"
# The two *_dict.json files are NOT in any GPT-SoVITS repo - they come from the
# upstream g2pW project. Omitting them makes every Chinese synthesis fail with
# 400 "Files not found", which is the single most confusing failure in this setup
# because the server starts up looking perfectly healthy.
$G2PWUP = 'https://gh-proxy.com/https://raw.githubusercontent.com/GitYCC/g2pW/master/g2pw'
# Chinese-trained character pack (Genshin + Star Rail). See §3 of the design doc.
$MIHOYO = "$HF/BigPancake01/GPT-SoVITS_Mihoyo/resolve/main"

# 每项: Url, 本地相对路径（相对 $Root）, 说明
# 本地路径按 tts_infer.yaml 期望的位置摆放。
$Items = @(
    # --- 源码 ---
    [pscustomobject]@{ Group='src'; Url='https://gh-proxy.com/https://github.com/baicai-1145/GPT-SoVITS-CPUFast/archive/refs/heads/main.zip'; Local='__SOURCE_ZIP__'; Note='CPUFast 源码包 7.2MB（gh-proxy.com 实测可下完；ghproxy.net 会中途断）' }

    # --- 底模：HuBERT（内容编码器）---
    [pscustomobject]@{ Group='base'; Url="$PRE/pretrained_models/chinese-hubert-base/pytorch_model.bin"; Local='GPT_SoVITS/pretrained_models/chinese-hubert-base/pytorch_model.bin'; Note='HuBERT 180MB' }
    [pscustomobject]@{ Group='base'; Url="$PRE/pretrained_models/chinese-hubert-base/config.json"; Local='GPT_SoVITS/pretrained_models/chinese-hubert-base/config.json'; Note='HuBERT 配置' }
    [pscustomobject]@{ Group='base'; Url="$PRE/pretrained_models/chinese-hubert-base/preprocessor_config.json"; Local='GPT_SoVITS/pretrained_models/chinese-hubert-base/preprocessor_config.json'; Note='HuBERT 配置' }

    # --- 底模：RoBERTa（中文文本编码器）---
    [pscustomobject]@{ Group='base'; Url="$PRE/pretrained_models/chinese-roberta-wwm-ext-large/pytorch_model.bin"; Local='GPT_SoVITS/pretrained_models/chinese-roberta-wwm-ext-large/pytorch_model.bin'; Note='RoBERTa 621MB（最大的单个文件）' }
    [pscustomobject]@{ Group='base'; Url="$PRE/pretrained_models/chinese-roberta-wwm-ext-large/config.json"; Local='GPT_SoVITS/pretrained_models/chinese-roberta-wwm-ext-large/config.json'; Note='RoBERTa 配置' }
    [pscustomobject]@{ Group='base'; Url="$PRE/pretrained_models/chinese-roberta-wwm-ext-large/tokenizer.json"; Local='GPT_SoVITS/pretrained_models/chinese-roberta-wwm-ext-large/tokenizer.json'; Note='RoBERTa 词表 0.26MB' }

    # --- 底模：语言检测 ---
    [pscustomobject]@{ Group='base'; Url="$PRE/pretrained_models/fast_langdetect/lid.176.bin"; Local='GPT_SoVITS/pretrained_models/fast_langdetect/lid.176.bin'; Note='语言检测 125MB' }
    [pscustomobject]@{ Group='base'; Url="$PRE/pretrained_models/fast_langdetect/lid.176.ftz"; Local='GPT_SoVITS/pretrained_models/fast_langdetect/lid.176.ftz'; Note='语言检测（压缩版）0.9MB' }

    # --- 底模：v2 推理权重（本方案用 v2）---
    [pscustomobject]@{ Group='base'; Url="$PRE/pretrained_models/gsv-v2final-pretrained/s1bert25hz-5kh-longer-epoch=12-step=369668.ckpt"; Local='GPT_SoVITS/pretrained_models/gsv-v2final-pretrained/s1bert25hz-5kh-longer-epoch=12-step=369668.ckpt'; Note='v2 GPT 底模 148MB' }
    [pscustomobject]@{ Group='base'; Url="$PRE/pretrained_models/gsv-v2final-pretrained/s2G2333k.pth"; Local='GPT_SoVITS/pretrained_models/gsv-v2final-pretrained/s2G2333k.pth'; Note='v2 SoVITS 底模 101MB' }

    # --- G2PW：中文多音字 ---
    [pscustomobject]@{ Group='g2pw'; Url="$G2PW/g2pw.pth"; Local='GPT_SoVITS/text/G2PWModel/g2pw.pth'; Note='G2PW 权重 608MB' }
    [pscustomobject]@{ Group='g2pw'; Url="$G2PW/config.py"; Local='GPT_SoVITS/text/G2PWModel/config.py'; Note='G2PW 配置' }
    [pscustomobject]@{ Group='g2pw'; Url="$G2PW/MONOPHONIC_CHARS.txt"; Local='GPT_SoVITS/text/G2PWModel/MONOPHONIC_CHARS.txt'; Note='G2PW 字表' }
    [pscustomobject]@{ Group='g2pw'; Url="$G2PW/POLYPHONIC_CHARS.txt"; Local='GPT_SoVITS/text/G2PWModel/POLYPHONIC_CHARS.txt'; Note='G2PW 字表' }
    [pscustomobject]@{ Group='g2pw'; Url="$G2PW/record.log"; Local='GPT_SoVITS/text/G2PWModel/record.log'; Note='G2PW 记录文件' }
    [pscustomobject]@{ Group='g2pw'; Url="$G2PWUP/bopomofo_to_pinyin_wo_tune_dict.json"; Local='GPT_SoVITS/text/G2PWModel/bopomofo_to_pinyin_wo_tune_dict.json'; Note='*** 中文合成必需 7.6KB（缺则所有中文请求 400）' }
    [pscustomobject]@{ Group='g2pw'; Url="$G2PWUP/char_bopomofo_dict.json"; Local='GPT_SoVITS/text/G2PWModel/char_bopomofo_dict.json'; Note='*** 中文合成必需 1.8MB（缺则所有中文请求 400）' }

    # --- 可选：nltk / open_jtalk ---
    [pscustomobject]@{ Group='extra'; Url="$PRE/nltk_data.zip"; Local='__NLTK_ZIP__'; Note='nltk 数据 9.5MB（英文 g2p 用）' }
    [pscustomobject]@{ Group='extra'; Url="$PRE/open_jtalk_dic_utf_8-1.11.tar.gz"; Local='__OJTALK__'; Note='open_jtalk 词典 22.7MB（仅日文需要，中文可跳过）' }

    # --- 角色模型：中文语料训练（推荐）---
    #
    # BigPancake01/GPT-SoVITS_Mihoyo：README 明确「使用…角色的对话语音训练」，
    # HF 语言标注 zh。已验证可莉/纳西妲/绫华/宵宫四角色中文效果正常。
    #
    # 注意：**不要用 xiaoheiqaq/GPT-Sovits-models 的心海**。那个模型是英语配音
    # 训练的（README: "Trained on ~1hr of english voiceover data"），合成中文
    # 完全不像——这是实测过的，详见 docs/genshin-tts.md §16.4.0(1)。
    #
    # 文件名要照抄：绫华的 VITS 带 _315 后缀，漏掉会 404。
    [pscustomobject]@{ Group='char'; Url="$MIHOYO/GPT_models/Klee/Klee-e10.ckpt";              Local='models/klee/gpt.ckpt';      Note='可莉 GPT e10 148MB' }
    [pscustomobject]@{ Group='char'; Url="$MIHOYO/VITS_models/Klee/Klee_e8_s656.pth";         Local='models/klee/sovits.pth';   Note='可莉 SoVITS e8 81MB' }
    [pscustomobject]@{ Group='char'; Url="$MIHOYO/ref_audios/Klee.wav";                       Local='models/klee/ref.wav';      Note='可莉 参考音频 0.7MB' }
    [pscustomobject]@{ Group='char'; Url="$MIHOYO/GPT_models/Nahida/Nahida-e10.ckpt";         Local='models/nahida/gpt.ckpt';   Note='纳西妲 GPT e10 148MB' }
    [pscustomobject]@{ Group='char'; Url="$MIHOYO/VITS_models/Nahida/Nahida_e8_s1200.pth";    Local='models/nahida/sovits.pth'; Note='纳西妲 SoVITS e8 81MB' }
    [pscustomobject]@{ Group='char'; Url="$MIHOYO/ref_audios/Nahida.wav";                     Local='models/nahida/ref.wav';    Note='纳西妲 参考音频 0.7MB' }
    [pscustomobject]@{ Group='char'; Url="$MIHOYO/GPT_models/Ayaka/Ayaka-e10.ckpt";           Local='models/ayaka/gpt.ckpt';    Note='绫华 GPT e10 148MB' }
    [pscustomobject]@{ Group='char'; Url="$MIHOYO/VITS_models/Ayaka/Ayaka_315_e8_s576.pth";   Local='models/ayaka/sovits.pth';  Note='绫华 SoVITS e8 81MB（注意 _315）' }
    [pscustomobject]@{ Group='char'; Url="$MIHOYO/ref_audios/Ayaka.wav";                      Local='models/ayaka/ref.wav';     Note='绫华 参考音频 0.5MB' }
    [pscustomobject]@{ Group='char'; Url="$MIHOYO/GPT_models/Yoimiya/Yoimiya-e5.ckpt";        Local='models/yoimiya/gpt.ckpt';  Note='宵宫 GPT e5 148MB（只有 e5）' }
    [pscustomobject]@{ Group='char'; Url="$MIHOYO/VITS_models/Yoimiya/Yoimiya_e8_s1024.pth";  Local='models/yoimiya/sovits.pth'; Note='宵宫 SoVITS e8 81MB' }
    [pscustomobject]@{ Group='char'; Url="$MIHOYO/ref_audios/Yoimiya.wav";                    Local='models/yoimiya/ref.wav';   Note='宵宫 参考音频 0.8MB' }
)

$wanted = if ($Group -eq 'all') { $Items } else { $Items | Where-Object { $_.Group -eq $Group } }

# --- 输出 -------------------------------------------------------------------

Write-Host ""
Write-Host "P0 需要下载的文件（已逐一验证可访问）" -ForegroundColor White
Write-Host "安装目录: $Root" -ForegroundColor DarkGray
Write-Host "==========================================================" -ForegroundColor DarkGray

$total = 0
$byGroup = $wanted | Group-Object Group
foreach ($g in $byGroup) {
    Write-Host ""
    Write-Host "[$($g.Name)]" -ForegroundColor Cyan
    foreach ($it in $g.Group) {
        Write-Host "  $($it.Note)" -ForegroundColor White
        Write-Host "    $($it.Url)" -ForegroundColor Gray
        if ($it.Local -notlike '__*__') {
            Write-Host "    -> $Root\$($it.Local -replace '/','\')" -ForegroundColor DarkGray
        }
    }
}

Write-Host ""
Write-Host "==========================================================" -ForegroundColor DarkGray
Write-Host "不包含 4.35GB 的 pretrained_models.zip：它含 v1/v3/v4/v2Pro 权重，v2 用不到。" -ForegroundColor Yellow
Write-Host "下面这些是 v2 的最小集合，合计约 2GB：base 1.04GB + g2pw 0.61GB + char 0.23GB" -ForegroundColor Yellow
Write-Host "（extra 的 nltk/open_jtalk 是可选的，中文输出用不到 open_jtalk）" -ForegroundColor Yellow

if (-not $Download) {
    Write-Host ""
    Write-Host "这只是清单。要真的下载，加 -Download：" -ForegroundColor White
    Write-Host "  powershell -ExecutionPolicy Bypass -File tools\tts-p0\fetch.ps1 -Download -Group base" -ForegroundColor Gray
    Write-Host ""
    Write-Host "用下载器工具（IDM/aria2/迅雷）的话，直接复制上面的 URL 即可。" -ForegroundColor Gray
    exit 0
}

# --- 下载 -------------------------------------------------------------------

Write-Host ""
Write-Host "开始下载..." -ForegroundColor Cyan

foreach ($it in $wanted) {
    if ($it.Local -eq '__SOURCE_ZIP__') {
        $dest = Join-Path $env:TEMP 'gptsovits-cpufast.zip'
    } elseif ($it.Local -eq '__NLTK_ZIP__') {
        $dest = Join-Path $Root 'nltk_data.zip'
    } elseif ($it.Local -eq '__OJTALK__') {
        $dest = Join-Path $Root 'open_jtalk_dic_utf_8-1.11.tar.gz'
    } else {
        $dest = Join-Path $Root ($it.Local -replace '/', '\')
    }

    $dir = Split-Path -Parent $dest
    if ($dir -and -not (Test-Path $dir)) { New-Item -ItemType Directory -Force -Path $dir | Out-Null }

    if (Test-Path $dest) {
        $sz = [math]::Round((Get-Item $dest).Length / 1MB, 1)
        Write-Host "  [skip] 已存在 $sz MB : $($it.Local)" -ForegroundColor DarkGray
        continue
    }

    Write-Host "  [get ] $($it.Note)" -ForegroundColor White
    # -C - 断点续传：网络中断后重跑本脚本会接着下，而不是从头来。
    & curl.exe -L -C - --retry 5 --retry-delay 3 --retry-all-errors `
        --max-time 3600 -o $dest $it.Url
    if ($LASTEXITCODE -ne 0) {
        Write-Host "  [FAIL] curl 退出码 $LASTEXITCODE ：$($it.Url)" -ForegroundColor Red
        Write-Host "         重新跑本脚本会从这个文件的断点继续。" -ForegroundColor Yellow
        Write-Host "         反复失败就换成下载器工具手动抓这个 URL。" -ForegroundColor Yellow
        exit 1
    }
    $sz = [math]::Round((Get-Item $dest).Length / 1MB, 1)
    $total += (Get-Item $dest).Length
    Write-Host "  [ok  ] $sz MB" -ForegroundColor Green
}

Write-Host ""
Write-Host "下载完成，共 $([math]::Round($total/1GB,2))GB" -ForegroundColor Green
Write-Host ""
Write-Host "接下来：" -ForegroundColor White
Write-Host "  1) 若下过源码 zip，解压后把里面那层目录的内容放到 $Root" -ForegroundColor Gray
Write-Host "  2) 跑 install.ps1（它检测到文件已存在会跳过下载，只建环境和装依赖）" -ForegroundColor Gray
Write-Host "     powershell -ExecutionPolicy Bypass -File tools\tts-p0\install.ps1 -Root $Root" -ForegroundColor Gray

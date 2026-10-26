<#
.SYNOPSIS
    测量本地 GPT-SoVITS 合成服务的速度与内存占用（P0 的核心产物）。

.DESCRIPTION
    这个脚本回答一个问题，也是 docs/genshin-tts.md §9 要求先测的那三个数：

      1. 冷启动时间  —— 从启动服务到 /tts 第一次可用
      2. 单句 RTF    —— 合成耗时 ÷ 音频时长（1.0 = 与实时等速）
      3. 峰值内存    —— 合成时服务进程占用的内存

    它不做任何"评估"或"猜测"，只把数字测出来并对照判定标准。

    判定标准（docs/genshin-tts.md §9）：

      RTF ≤ 1.5     可以按原方案做，体验可接受
      1.5 ~ 2.5     只朗读主动说话（≤80字）与番茄钟，不读长回复
      > 2.5         放弃实时合成，改走"台词库"方案

    参考基线：官方 README 给出 M4 CPU RTF 0.526。这台机器是 i5-11300H，
    但实测 AVX-512 + VNNI 都可用（Tiger Lake 多数型号会熔断，这颗没有），
    所以结果有待实测。

.PARAMETER Root
    GPT-SoVITS 安装目录（与 install.ps1 的 -Root 一致）。

.PARAMETER Character
    角色模型名，用于定位 install.ps1 生成的 petpc-config.json。默认 kokomi。

.PARAMETER BaseUrl
    合成服务地址。默认 http://127.0.0.1:9880。

.PARAMETER StartServer
    自己启动服务并测量冷启动。不加这个开关时，脚本假定服务已在运行
    （你自己起的），只测 RTF 与内存。

.PARAMETER Port
    服务端口，默认 9880。

.EXAMPLE
    # 最省事：脚本自己起服务，测完自动关掉
    pwsh -File tools\tts-p0\benchmark.ps1 -Root D:\GPT-SoVITS -StartServer

.EXAMPLE
    # 服务已经在另一个终端跑着，只测速度
    pwsh -File tools\tts-p0\benchmark.ps1 -Root D:\GPT-SoVITS
#>

[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$Root,
    [string]$Character = 'kokomi',
    [string]$BaseUrl = 'http://127.0.0.1:9880',
    [switch]$StartServer,
    [int]$Port = 9880,
    # 每个句子重复测几次，取最好的一次（第一次含各种冷路径）。
    [int]$Repeat = 3
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

function Write-Step($m) { Write-Host ""; Write-Host "==> $m" -ForegroundColor Cyan }
function Write-Ok($m) { Write-Host "    [OK]   $m" -ForegroundColor Green }
function Write-Warn2($m) { Write-Host "    [WARN] $m" -ForegroundColor Yellow }
function Write-Err($m) { Write-Host "    [FAIL] $m" -ForegroundColor Red }
function Write-Note($m) { Write-Host "           $m" -ForegroundColor DarkGray }

# Test-ServerAlive reports whether something is speaking HTTP at $Url.
#
# ANY HTTP status counts, including 5xx. That is deliberate and was learned from
# the real server: a bare `GET /tts` with no query parameters raises
# AttributeError ('NoneType' has no attribute 'lower') inside api_v2.py, so it
# answers 500 rather than the 400 a parameter-validation-first implementation would
# give. Treating only 4xx as "alive" therefore reported a perfectly healthy server
# as unreachable.
#
# What distinguishes "alive" from "down" is whether an HTTP response arrived at
# all: a refused connection or a timeout throws, an error status does not.
function Test-ServerAlive([string]$Url) {
    try {
        $null = Invoke-WebRequest -Uri "$Url/tts" -Method Get -TimeoutSec 8 -UseBasicParsing -ErrorAction Stop
        return $true
    } catch {
        # A response object of any status means the server answered.
        return [bool]$_.Exception.Response
    }
}

Write-Host ""
Write-Host "PetPC 语音朗读 · P0 基准测试" -ForegroundColor White
Write-Host "==========================================================" -ForegroundColor DarkGray

# --- 找出角色配置 -----------------------------------------------------------

Write-Step '读取角色配置'

$cfgPath = Join-Path $Root "models\$Character\petpc-config.json"
if (-not (Test-Path $cfgPath)) {
    Write-Err "没有找到 $cfgPath"
    Write-Note '先跑 install.ps1，或手动指定一个已知的参考音频与权重。'
    exit 1
}
$cfg = Get-Content $cfgPath -Raw -Encoding UTF8 | ConvertFrom-Json

$refAudio   = $cfg.ttsRefAudioPath
$promptText = $cfg.ttsPromptText
$gptWeights = $cfg.ttsGptWeights
$sovWeights = $cfg.ttsSovitsWeights
$lang       = if ($cfg.ttsLang) { $cfg.ttsLang } else { 'zh' }

foreach ($f in @($refAudio, $gptWeights, $sovWeights)) {
    if (-not (Test-Path $f)) {
        Write-Err "配置里引用的文件不存在：$f"
        exit 1
    }
}
Write-Ok "参考音频: $(Split-Path -Leaf $refAudio)"
Write-Ok "GPT 权重: $(Split-Path -Leaf $gptWeights)"
Write-Ok "SoVITS 权重: $(Split-Path -Leaf $sovWeights)"
Write-Ok "语言: $lang"

# --- 启动服务（可选）--------------------------------------------------------

$serverProc = $null
$startupMs = $null

if ($StartServer) {
    Write-Step '启动合成服务并测量冷启动'

    if ($BaseUrl -notmatch [regex]::Escape(":$Port")) {
        Write-Warn2 "-BaseUrl 与 -Port 看起来不一致，按 -Port $Port 启动"
    }
    $upp = Join-Path $Root '.venv\Scripts\python.exe'
    if (-not (Test-Path $upp)) {
        Write-Err "没有找到 $upp"
        exit 1
    }

    # 先把端口占用检查掉，否则会把"上一轮没关干净的服务"当成新启动的。
    $existing = Get-NetTCPConnection -LocalPort $Port -State Listen -ErrorAction SilentlyContinue
    if ($existing) {
        Write-Err "端口 $Port 已被占用（PID $($existing.OwningProcess)）"
        Write-Note '先关掉那个进程，或换一个 -Port。'
        exit 1
    }

    $logOut = Join-Path $env:TEMP 'petpc-tts-server.out.log'
    $logErr = Join-Path $env:TEMP 'petpc-tts-server.err.log'
    Remove-Item $logOut, $logErr -Force -ErrorAction SilentlyContinue

    $sw = [System.Diagnostics.Stopwatch]::StartNew()
    # -c 指向 tts_infer.yaml，里面用相对路径引用底模，所以工作目录必须是 $Root。
    try {
        $serverProc = Start-Process -FilePath $upp `
            -ArgumentList @('api_v2.py', '-a', '127.0.0.1', '-p', "$Port", '-c', 'GPT_SoVITS\configs\tts_infer.yaml') `
            -WorkingDirectory $Root `
            -RedirectStandardOutput $logOut -RedirectStandardError $logErr `
            -PassThru -NoNewWindow
    } catch {
        # Start-Process raises a raw, unfriendly error here (a venv python that is
        # not really an executable, or a policy that blocks it). Translate it into
        # something the user can act on.
        Write-Err "无法启动服务：$($_.Exception.Message)"
        Write-Note "命令：`"$upp`" api_v2.py -a 127.0.0.1 -p $Port -c GPT_SoVITS\configs\tts_infer.yaml"
        Write-Note "工作目录：$Root"
        Write-Note '可以先手动跑上面这条命令确认服务能起来，再用不带 -StartServer 的方式测速。'
        exit 1
    }

    Write-Note "PID $($serverProc.Id)，等待 /tts 变为可用..."
    Write-Note '（首次启动要加载底模，可能十几秒到一分钟）'

    # 用一个"缺参数"的探针请求判断服务是否起来了：服务活着时它会回一个 HTTP
    # 错误码（实测这个 CPUFast 版本回 500，不是 400）而不是连接被拒。
    # 判据是"有没有 HTTP 应答"，不是"错误码落在哪个区间"。
    $ready = $false
    $deadline = (Get-Date).AddMinutes(5)
    while ((Get-Date) -lt $deadline) {
        if ($serverProc.HasExited) {
            Write-Err "服务进程已退出（退出码 $($serverProc.ExitCode)）"
            Write-Note "stderr 末尾："
            if (Test-Path $logErr) { Get-Content $logErr -Tail 20 | ForEach-Object { Write-Host "      $_" -ForegroundColor DarkRed } }
            exit 1
        }
        if (Test-ServerAlive $BaseUrl) { $ready = $true; break }
        Start-Sleep -Milliseconds 500
    }
    $sw.Stop()

    if (-not $ready) {
        Write-Err '服务在 5 分钟内没有变为可用'
        if (Test-Path $logErr) { Get-Content $logErr -Tail 20 | ForEach-Object { Write-Host "      $_" -ForegroundColor DarkRed } }
        if ($serverProc -and -not $serverProc.HasExited) { Stop-Process -Id $serverProc.Id -Force -ErrorAction SilentlyContinue }
        exit 1
    }
    $startupMs = $sw.ElapsedMilliseconds
    Write-Ok "冷启动到可用：$startupMs ms"
} else {
    Write-Step '检查服务是否已在运行'
    if (-not (Test-ServerAlive $BaseUrl)) {
        Write-Err "连不上 $BaseUrl"
        Write-Note '加 -StartServer 让脚本自己起服务，或先在另一个终端启动它。'
        exit 1
    }
    Write-Ok '服务在运行'
}

# --- 设定角色权重 -----------------------------------------------------------

Write-Step '推送角色权重'

# 服务可能加载的是它自己 config 里的默认音色，先把要测的角色推上去，
# 否则测的是别的音色（速度会不同，结论就不对了）。
function Set-Weights($path, $endpoint) {
    # The weights_path query parameter is REQUIRED by api_v2.py; omitting it makes
    # the endpoint answer 400 "gpt weight path is required" and the character is
    # never loaded - so the benchmark would silently measure the server's own
    # default voice instead of the one under test.
    $enc = [uri]::EscapeDataString($path)
    try {
        $r = Invoke-WebRequest -Uri "$BaseUrl$endpoint`?weights_path=$enc" -Method Get -UseBasicParsing -TimeoutSec 120 -ErrorAction Stop
        Write-Note "$endpoint -> $($r.StatusCode)"
    } catch {
        Write-Warn2 "$endpoint 失败：$($_.Exception.Message)"
    }
}
Set-Weights $gptWeights '/set_gpt_weights'
Set-Weights $sovWeights '/set_sovits_weights'

# --- RTF 测量 ---------------------------------------------------------------

Write-Step '测量合成速度（RTF = 合成耗时 ÷ 音频时长）'

# 三档长度，分别代表：番茄钟提示 / 主动说话上限 / 一段较长的回复。
$samples = @(
    @{ Name = '短句 8 字';   Text = '番茄钟结束了。' }
    @{ Name = '中句 31 字';  Text = '你好，我是你的桌宠，现在有三件事情今天到期，先从哪一件开始呢？' }
    @{ Name = '长句 63 字';  Text = '你已经连续专注了二十五分钟，休息一下吧。起身走两步，喝口水，看看窗外远处的东西，让眼睛放松一会儿。' }
    @{ Name = '一段 108 字'; Text = '今天是十月二十四日星期五。你一共有五件待办，其中两件今天到期。上午的复习计划还没有开始，下午三点有一个会议。刚才那个番茄钟你完成了二十五分钟的专注，累计专注时长已经到了两小时十五分钟。' }
)

# WAV 时长必须从文件头里读 byteRate 来算，不能假定采样率。
#
# 最初的版本写死了 22.05kHz，而 api_v2.py 实际输出的是 **32kHz 单声道**，
# 于是报出的时长偏长约 45%、RTF 偏小约 31% —— 也就是说它把自己的测量对象
# 夸好了三分之一。一个会美化被测量对象的基准比没有基准更糟。
#
# 这里的算法与 PetPC 的 internal/tts/wav.go 一致：遍历 chunk 找到 fmt 块拿
# byteRate，再从 data 块大小算时长。
function Get-WavSecondsFromBytes([byte[]]$data) {
    if ($null -eq $data -or $data.Length -lt 44) { return 0 }
    $riff = [System.Text.Encoding]::ASCII.GetString($data, 0, 4)
    $wave = [System.Text.Encoding]::ASCII.GetString($data, 8, 4)
    if ($riff -ne 'RIFF' -or $wave -ne 'WAVE') { return 0 }

    $pos = 12
    $byteRate = 0
    $dataSize = 0
    while ($pos + 8 -le $data.Length) {
        $id   = [System.Text.Encoding]::ASCII.GetString($data, $pos, 4)
        $size = [BitConverter]::ToUInt32($data, $pos + 4)
        $body = $pos + 8
        if ($id -eq 'fmt ' -and $size -ge 16 -and $body + 12 -le $data.Length) {
            $byteRate = [BitConverter]::ToUInt32($data, $body + 8)
        }
        if ($id -eq 'data') { $dataSize = $size; break }
        # Chunk 按字对齐，奇数长度后面补一个字节。
        $adv = $size
        if ($adv % 2 -eq 1) { $adv++ }
        if ($adv -le 0) { break }
        $pos = $body + $adv
    }
    if ($byteRate -le 0 -or $dataSize -le 0) { return 0 }
    return [double]$dataSize / [double]$byteRate
}

$results = @()
foreach ($s in $samples) {
    $best = $null
    for ($i = 1; $i -le $Repeat; $i++) {
        $json = @{
            text              = $s.Text
            text_lang         = $lang
            ref_audio_path    = $refAudio
            prompt_text       = $promptText
            prompt_lang       = $lang
            media_type        = 'wav'
            streaming_mode    = $false
        } | ConvertTo-Json -Compress

        # The body MUST be passed as an explicit UTF-8 byte array.
        #
        # Windows PowerShell 5.1 encodes a string body as ASCII/Latin-1, which
        # silently mangles every Chinese character into '?' - and the server then
        # answers 400 "JSON decode error" for what looks like a perfectly good
        # request. Handing it pre-encoded bytes is what avoids that; it is also why
        # curl (which always sends the file's bytes verbatim) worked while this
        # script did not.
        $bodyBytes = [System.Text.Encoding]::UTF8.GetBytes($json)

        # Write the response to a temp file rather than reading .Content.
        #
        # PowerShell 5.1 decodes .Content as a string, which corrupts binary audio
        # (the WAV header survives but the sample bytes do not), and its
        # RawContentLength is not always populated. -OutFile writes the bytes
        # verbatim, which is the only form this can be trusted in.
        $tmpWav = Join-Path $env:TEMP "petpc-bench-$([guid]::NewGuid().ToString('N')).wav"
        $sw = [System.Diagnostics.Stopwatch]::StartNew()
        try {
            Invoke-WebRequest -Uri "$BaseUrl/tts" -Method Post -Body $bodyBytes `
                -ContentType 'application/json; charset=utf-8' `
                -OutFile $tmpWav -TimeoutSec 300 -UseBasicParsing -ErrorAction Stop
            $resp = $true
        } catch {
            Write-Err "合成失败（$($s.Name) 第 $i 次）：$($_.Exception.Message)"
            $resp = $false
        }
        $sw.Stop()
        if (-not $resp) { continue }

        $wav = [System.IO.File]::ReadAllBytes($tmpWav)
        Remove-Item $tmpWav -Force -ErrorAction SilentlyContinue
        $soundSec = Get-WavSecondsFromBytes $wav
        if ($soundSec -le 0) { continue }
        $rtf = ($sw.Elapsed.TotalSeconds) / $soundSec
        if ($null -eq $best -or $rtf -lt $best.Rtf) {
            $best = [pscustomobject]@{
                Name      = $s.Name
                Runes     = $s.Text.Length
                SynthMs   = [math]::Round($sw.Elapsed.TotalMilliseconds)
                AudioSec  = [math]::Round($soundSec, 2)
                AudioKB   = [math]::Round($wav.Length / 1KB)
                Rtf       = [math]::Round($rtf, 3)
            }
        }
    }
    if ($best) {
        $results += $best
        Write-Ok ("{0,-12} {1,3}字 合成 {2,6}ms  音频 {3,5}s  RTF {4,5}" -f `
            $best.Name, $best.Runes, $best.SynthMs, $best.AudioSec, $best.Rtf)
    }
}

if ($results.Count -eq 0) {
    Write-Err '没有任何一次合成成功，无法得出结论'
    if ($serverProc -and -not $serverProc.HasExited) { Stop-Process -Id $serverProc.Id -Force -ErrorAction SilentlyContinue }
    exit 1
}

# --- 内存 -------------------------------------------------------------------

Write-Step '测量服务进程内存'

# Sum EVERY python process serving the API, not just one.
#
# Two things make a single-process reading wrong here, and both were observed on
# the real machine:
#
#   1. uv's venv launcher spawns a CHILD python that does the actual work. The
#      parent sits at ~4MB while the child holds ~3.4GB, so picking the first match
#      reported "4 MB" for a server using three gigabytes.
#   2. Newer CPython versions can split work across more than one process.
#
# Reporting the total is also the number that matters: this is memory the user's
# machine no longer has available, whichever process holds it.
$peakMB = $null
$servers = @(Get-CimInstance Win32_Process -Filter "Name='python.exe'" -ErrorAction SilentlyContinue |
             Where-Object { $_.CommandLine -match 'api_v2' })
if ($servers.Count -gt 0) {
    $totalBytes = 0
    foreach ($srv in $servers) {
        $proc = Get-Process -Id $srv.ProcessId -ErrorAction SilentlyContinue
        if (-not $proc) { continue }
        $totalBytes += $proc.WorkingSet64
        Write-Note "PID $($srv.ProcessId): $([math]::Round($proc.WorkingSet64/1MB)) MB"
    }
    if ($totalBytes -gt 0) { $peakMB = [math]::Round($totalBytes / 1MB) }
}
if ($peakMB) {
    Write-Ok "服务进程工作集合计：$peakMB MB"
} else {
    Write-Warn2 '读不到服务进程内存（跨权限或进程已退出）'
    Write-Note '可以打开任务管理器看那些 python.exe 的内存。'
}

# 系统可用内存：CPU 推理会让机器很吃紧，这个数字决定"开着合成服务还能不能正常用电脑"。
$availMB = $null
try {
    if (-not ('PetPC.MemStatus' -as [type])) {
        Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
namespace PetPC {
    public class MemStatus {
        [StructLayout(LayoutKind.Sequential)]
        public struct MEMORYSTATUSEX {
            public uint dwLength; public uint dwMemoryLoad;
            public ulong ullTotalPhys; public ulong ullAvailPhys;
            public ulong ullTotalPageFile; public ulong ullAvailPageFile;
            public ulong ullTotalVirtual; public ulong ullAvailVirtual;
            public ulong ullAvailExtendedVirtual;
        }
        [DllImport("kernel32.dll", SetLastError = true)]
        public static extern bool GlobalMemoryStatusEx(ref MEMORYSTATUSEX m);
    }
}
'@
    }
    $ms = New-Object PetPC.MemStatus+MEMORYSTATUSEX
    $ms.dwLength = [System.Runtime.InteropServices.Marshal]::SizeOf($ms)
    if ([PetPC.MemStatus]::GlobalMemoryStatusEx([ref]$ms)) {
        $availMB = [math]::Round($ms.ullAvailPhys / 1MB)
        Write-Ok "系统可用内存：$availMB MB（负载 $($ms.dwMemoryLoad)%）"
        if ($availMB -lt 1024) {
            Write-Warn2 "可用内存不足 1GB —— 合成服务占用很重，建议朗读时关掉浏览器等大户"
        }
    }
} catch {
    Write-Note "读不到系统内存：$($_.Exception.Message)"
}

# --- 判定 -------------------------------------------------------------------

$avgRtf = ($results | Measure-Object -Property Rtf -Average).Average
$maxRtf = ($results | Measure-Object -Property Rtf -Maximum).Maximum

Write-Host ""
Write-Host "==========================================================" -ForegroundColor DarkGray
Write-Host " 结果" -ForegroundColor White
Write-Host "==========================================================" -ForegroundColor DarkGray
Write-Host ""
if ($startupMs) { Write-Host ("  冷启动到可用 : {0} ms" -f $startupMs) -ForegroundColor White }
if ($peakMB)    { Write-Host ("  服务内存峰值 : {0} MB" -f $peakMB) -ForegroundColor White }
Write-Host ("  RTF 平均     : {0}" -f [math]::Round($avgRtf, 3)) -ForegroundColor White
Write-Host ("  RTF 最差     : {0}" -f [math]::Round($maxRtf, 3)) -ForegroundColor White
Write-Host ""

# 判定表来自 docs/genshin-tts.md §9。
if ($maxRtf -le 1.5) {
    Write-Host "  判定：可以按原方案做 —— RTF ≤ 1.5，体验可接受。" -ForegroundColor Green
    Write-Host "        首句延迟约等于一句的合成时间，整段回复可以边生成边朗读。" -ForegroundColor Green
} elseif ($maxRtf -le 2.5) {
    Write-Host "  判定：缩范围 —— RTF 在 1.5~2.5。" -ForegroundColor Yellow
    Write-Host "        建议只朗读主动说话（≤80字）与番茄钟提示，不读长回复；" -ForegroundColor Yellow
    Write-Host "        并把 TTSMaxSentences 调小（比如 2）。" -ForegroundColor Yellow
} else {
    Write-Host "  判定：建议放弃实时合成 —— RTF > 2.5。" -ForegroundColor Red
    Write-Host "        改走 docs/genshin-tts.md §12 的「台词库」方案：" -ForegroundColor Red
    Write-Host "        把固定文案预生成 WAV，自由文本不朗读。" -ForegroundColor Red
}

Write-Host ""
Write-Host "  下一步：" -ForegroundColor White
Write-Host "    把这三个数（冷启动 / RTF / 内存）记到 docs\genshin-tts.md §16.4 的空缺处。" -ForegroundColor Gray
Write-Host ""

# --- 收尾 -------------------------------------------------------------------

if ($serverProc -and -not $serverProc.HasExited) {
    Write-Step '关闭脚本启动的服务'
    Stop-Process -Id $serverProc.Id -Force -ErrorAction SilentlyContinue
    Start-Sleep -Milliseconds 500
    Write-Ok '已关闭'
}

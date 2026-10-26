# Synthesise a comparison sample for each candidate character, using the real
# /tts contract, and write the WAVs to D:\GPT-SoVITS\试听 so they can be played.
#
# Why a script rather than curl: the request body holds Chinese text, and
# PowerShell 5.1's Invoke-WebRequest encodes a string body as ASCII (mangling it).
# Passing an explicit UTF-8 byte array is the fix that was already found in
# tools/tts-p0/benchmark.ps1; this reuses it.
#
# The prompt_text matters: GPT-SoVITS aligns the reference clip using it. Each
# ref_audio in the Mihoyo pack comes from that character's own dialogue, so the
# text below is what the clip actually says where known - a wrong transcript
# degrades the voice rather than erroring, which is easy to mistake for "this
# model is bad".
param(
    [string[]]$Characters = @('klee'),
    [string]$OutDir = 'D:\GPT-SoVITS\试听'
)

$ErrorActionPreference = 'Stop'
$BaseUrl = 'http://127.0.0.1:9880'
$Root = 'D:\GPT-SoVITS'

# Per-character reference transcript. These are the lines the clips contain.
$RefText = @{
    'klee'    = '炸弹？嘿嘿，可莉最喜欢炸弹了！'
    'nahida'  = '你好呀，我是纳西妲。'
    'ayaka'   = '你好，我是神里绫华。'
    'yoimiya' = '烟花，要开始了哦！'
}

# One neutral sentence per character, plus a longer one, so both the timbre and
# the prosody can be judged.
$Samples = @(
    @{ label = 'A_短句'; text = '番茄钟结束了，休息一下吧。' }
    @{ label = 'B_长句'; text = '你好，我是你的桌宠。现在有三件事情今天到期，先从哪一件开始呢？' }
)

New-Item -ItemType Directory -Force -Path $OutDir | Out-Null

foreach ($ch in $Characters) {
    $dir = Join-Path $Root "models\$ch"
    $gpt = Join-Path $dir 'gpt.ckpt'
    $sov = Join-Path $dir 'sovits.pth'
    $ref = Join-Path $dir 'ref.wav'
    foreach ($f in @($gpt, $sov, $ref)) {
        if (-not (Test-Path $f)) { throw "missing $f" }
    }

    Write-Host "`n=== $ch ===" -ForegroundColor Cyan

    # Push the character's weights first: this is what makes the next /tts call
    # speak in the right voice.
    foreach ($pair in @(@{ p = $gpt; e = '/set_gpt_weights' }, @{ p = $sov; e = '/set_sovits_weights' })) {
        $enc = [uri]::EscapeDataString($pair.p)
        try {
            $r = Invoke-WebRequest -Uri "$BaseUrl$($pair.e)?weights_path=$enc" -Method Get `
                -UseBasicParsing -TimeoutSec 300 -ErrorAction Stop
            Write-Host "  $($pair.e) -> $($r.StatusCode)"
        } catch {
            Write-Host "  $($pair.e) FAILED: $($_.Exception.Message)" -ForegroundColor Red
        }
    }

    $prompt = $RefText[$ch]
    if (-not $prompt) { $prompt = '你好。' }

    foreach ($s in $Samples) {
        $json = @{
            text           = $s.text
            text_lang      = 'zh'
            ref_audio_path = $ref
            prompt_text    = $prompt
            prompt_lang    = 'zh'
            media_type     = 'wav'
            streaming_mode = $false
        } | ConvertTo-Json -Compress

        $bytes = [System.Text.Encoding]::UTF8.GetBytes($json)
        $dest = Join-Path $OutDir "$($ch)_$($s.label).wav"
        $sw = [System.Diagnostics.Stopwatch]::StartNew()
        try {
            Invoke-WebRequest -Uri "$BaseUrl/tts" -Method Post -Body $bytes `
                -ContentType 'application/json; charset=utf-8' `
                -OutFile $dest -TimeoutSec 600 -UseBasicParsing -ErrorAction Stop
        } catch {
            Write-Host "  $($s.label) FAILED: $($_.Exception.Message)" -ForegroundColor Red
            continue
        }
        $sw.Stop()

        $wav = [System.IO.File]::ReadAllBytes($dest)
        $byteRate = [BitConverter]::ToUInt32($wav, 28)
        $sec = [math]::Round(($wav.Length - 44) / $byteRate, 2)
        $rtf = [math]::Round($sw.Elapsed.TotalSeconds / $sec, 2)
        Write-Host ("  {0}  {1}s audio  {2}ms synth  RTF {3}  -> {4}" -f `
            $s.label, $sec, $sw.ElapsedMilliseconds, $rtf, (Split-Path -Leaf $dest))
    }
}

Write-Host "`n输出目录: $OutDir" -ForegroundColor Green
Get-ChildItem $OutDir -Filter '*.wav' | Sort-Object Name | ForEach-Object {
    Write-Host ("  {0,8:N0} KB  {1}" -f ($_.Length / 1KB), $_.Name)
}

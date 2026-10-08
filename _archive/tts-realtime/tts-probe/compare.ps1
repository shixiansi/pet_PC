# Synthesise one sample per character using the corrected settings, and play them
# in sequence so the voices can be compared back to back.
#
# The settings that matter, both learned the hard way on this machine:
#   1. prompt_text MUST be empty unless it is the clip's exact wording. A guessed
#      transcript makes GPT-SoVITS align the reference to the wrong text and the
#      output degenerates into laughter.
#   2. The body must be an explicit UTF-8 byte array: Windows PowerShell 5.1 encodes
#      a string body as ASCII and mangles every Chinese character into '?'.
param(
    [string[]]$Characters = @('klee', 'nahida', 'ayaka', 'yoimiya'),
    [string]$Text = '你好，我是你的桌宠。现在有三件事情今天到期，先从哪一件开始呢？',
    [switch]$NoPlay
)

$ErrorActionPreference = 'Stop'
$BaseUrl = 'http://127.0.0.1:9880'
$Root = 'D:\GPT-SoVITS'
$OutDir = Join-Path $Root '试听'

Add-Type -TypeDefinition @'
using System;using System.Runtime.InteropServices;
public class SP {
  [DllImport("winmm.dll",CharSet=CharSet.Unicode)] public static extern bool PlaySound(string s, IntPtr h, uint f);
}
'@

New-Item -ItemType Directory -Force -Path $OutDir | Out-Null

$made = @()
foreach ($ch in $Characters) {
    $dir = Join-Path $Root "models\$ch"
    $gpt = Join-Path $dir 'gpt.ckpt'
    $sov = Join-Path $dir 'sovits.pth'
    $ref = Join-Path $dir 'ref.wav'
    if (-not (Test-Path $gpt) -or -not (Test-Path $sov) -or -not (Test-Path $ref)) {
        Write-Host "skip $ch (incomplete)" -ForegroundColor DarkGray
        continue
    }

    # Load the character, then synthesise.
    foreach ($pair in @(@{ p = $gpt; e = '/set_gpt_weights' }, @{ p = $sov; e = '/set_sovits_weights' })) {
        $enc = [uri]::EscapeDataString($pair.p)
        Invoke-WebRequest -Uri "$BaseUrl$($pair.e)?weights_path=$enc" -Method Get `
            -UseBasicParsing -TimeoutSec 300 -ErrorAction Stop | Out-Null
    }

    $json = @{
        text           = $Text
        text_lang      = 'zh'
        ref_audio_path = $ref
        prompt_text    = ''      # deliberately empty - see the header comment
        prompt_lang    = 'zh'
        media_type     = 'wav'
        streaming_mode = $false
    } | ConvertTo-Json -Compress

    $bytes = [System.Text.Encoding]::UTF8.GetBytes($json)
    $dest = Join-Path $OutDir "cmp_$ch.wav"
    $sw = [System.Diagnostics.Stopwatch]::StartNew()
    Invoke-WebRequest -Uri "$BaseUrl/tts" -Method Post -Body $bytes `
        -ContentType 'application/json; charset=utf-8' `
        -OutFile $dest -TimeoutSec 600 -UseBasicParsing -ErrorAction Stop
    $sw.Stop()

    $wav = [System.IO.File]::ReadAllBytes($dest)
    $byteRate = [BitConverter]::ToUInt32($wav, 28)
    $sec = [math]::Round(($wav.Length - 44) / $byteRate, 2)
    Write-Host ("  {0,-9} {1,5}s audio  {2,6}ms synth  RTF {3}" -f `
        $ch, $sec, $sw.ElapsedMilliseconds, [math]::Round($sw.Elapsed.TotalSeconds / $sec, 2))
    $made += @{ name = $ch; path = $dest; sec = $sec }
}

if ($NoPlay -or $made.Count -eq 0) {
    Write-Host "`n输出: $OutDir"
    $made | ForEach-Object { Write-Host "  $($_.path)" }
    exit 0
}

Write-Host "`n按顺序播放（每个角色之间停 1 秒）..." -ForegroundColor Cyan
foreach ($m in $made) {
    Write-Host "▶ $($m.name)" -ForegroundColor Yellow
    [void][SP]::PlaySound($m.path, [IntPtr]::Zero, 0x1 -bor 0x20000 -bor 0x2)
    Start-Sleep -Milliseconds ([int](($m.sec + 1.0) * 1000))
}
[void][SP]::PlaySound($null, [IntPtr]::Zero, 0)
Write-Host "播放完毕" -ForegroundColor Green

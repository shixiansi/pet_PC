# Start the online voice bridge for PetPC.
#
# The bridge publishes a GPT-SoVITS-shaped /tts on localhost and serves it from a
# public HuggingFace Space. PetPC therefore needs no knowledge of the online
# protocol: in the settings panel, switch 语音来源 to 在线 and set the address to
# http://127.0.0.1:9881.
#
# Usage:
#   powershell -ExecutionPolicy Bypass -File tools\tts-online\start-bridge.ps1
#   powershell -ExecutionPolicy Bypass -File tools\tts-online\start-bridge.ps1 -ListVoices
#   powershell -ExecutionPolicy Bypass -File tools\tts-online\start-bridge.ps1 -Speaker 可莉 -Port 9881
#
# Nothing here modifies PetPC: stopping the bridge simply makes the online backend
# unavailable, and the pet falls silent rather than breaking.

[CmdletBinding()]
param(
    # Which voice to speak with until the settings panel changes it.
    [string]$Speaker = '',
    [int]$Port = 9881,
    # The upstream Space. Override only if you know another one that still runs.
    [string]$Host_ = 'zomehwh-vits-uma-genshin-honkai.hf.space',
    [int]$CacheMB = 300,
    # Print the available voices and exit.
    [switch]$ListVoices
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$here = Split-Path -Parent $MyInvocation.MyCommand.Path
$bridge = Join-Path $here 'bridge.py'
if (-not (Test-Path $bridge)) { throw "missing $bridge" }

# Prefer the GPT-SoVITS venv: it already has `websockets`, so the bridge works
# without installing anything new. Fall back to a system Python with a clear
# message if that venv is absent.
$candidates = @(
    'D:\GPT-SoVITS\.venv\Scripts\python.exe',
    (Join-Path $env:LOCALAPPDATA 'Programs\Python\Python313\python.exe'),
    (Join-Path $env:LOCALAPPDATA 'Programs\Python\Python312\python.exe')
)
$python = $candidates | Where-Object { Test-Path $_ } | Select-Object -First 1
if (-not $python) {
    $python = (Get-Command python -ErrorAction SilentlyContinue).Source
}
if (-not $python) {
    throw "no python found. Install Python 3.10+, or run the GPT-SoVITS setup in tools\tts-p0."
}

# websockets is the bridge's only dependency, and the GPT-SoVITS venv has it.
& $python -c "import websockets" 2>$null
if ($LASTEXITCODE -ne 0) {
    Write-Host "websockets is missing; installing it..." -ForegroundColor Yellow
    & $python -m pip install --quiet websockets
    if ($LASTEXITCODE -ne 0) { throw "could not install websockets" }
}

$args = @($bridge, '--port', "$Port", '--host', $Host_, '--cache-mb', "$CacheMB")
if ($Speaker) { $args += @('--speaker', $Speaker) }
if ($ListVoices) { $args += '--list-voices' }

Write-Host "python : $python" -ForegroundColor DarkGray
Write-Host "bridge : http://127.0.0.1:$Port  ->  $Host_" -ForegroundColor DarkGray
if (-not $ListVoices) {
    Write-Host ""
    Write-Host "In PetPC: 设置 -> 语音朗读 -> 语音来源 = 在线，地址填 http://127.0.0.1:$Port" -ForegroundColor Cyan
    Write-Host "Press Ctrl+C to stop." -ForegroundColor DarkGray
    Write-Host ""
}

# -u keeps the bridge's own logging unbuffered, so progress is visible live
# instead of arriving in one block when the process exits.
& $python -u @args

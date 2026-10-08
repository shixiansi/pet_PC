# Builds both PetPC programs.
#
# They are two separate Wails applications in one repository, so building them is
# two commands in the right order - which is exactly the kind of thing that gets
# half-done by hand. The studio is built FIRST and its executable copied next to the
# pet's, because the pet's "打开配音工坊" button looks for it there.
#
# Usage:
#   pwsh -File tools\build-all.ps1
#   pwsh -File tools\build-all.ps1 -SkipFrontend   # reuse existing bundles

param(
    [switch]$SkipFrontend
)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
. (Join-Path $root '.dev\env.ps1')

$wails = Join-Path $root '.dev\bin\wails.exe'
if (-not (Test-Path $wails)) { throw "wails CLI not found at $wails" }

function Build-One {
    param([string]$Name, [string]$Dir)
    Write-Host "`n=== $Name ===" -ForegroundColor Cyan
    Push-Location $Dir
    try {
        # -s skips Wails' own frontend build, which is done separately below so both
        # bundles are produced the same way and a frontend failure is obvious.
        if ($SkipFrontend) {
            & $wails build -s -skipbindings
        } else {
            & $wails build -s
        }
        if ($LASTEXITCODE -ne 0) { throw "$Name build failed" }
    } finally {
        Pop-Location
    }
}

function Build-Frontend {
    param([string]$Dir)
    Push-Location $Dir
    try {
        npm run build
        if ($LASTEXITCODE -ne 0) { throw "frontend build failed in $Dir" }
    } finally {
        Pop-Location
    }
}

if (-not $SkipFrontend) {
    Write-Host "=== frontend: voice studio ===" -ForegroundColor Cyan
    Build-Frontend (Join-Path $root 'tools\voice-studio\frontend')
    Write-Host "=== frontend: pet ===" -ForegroundColor Cyan
    Build-Frontend (Join-Path $root 'frontend')
}

Build-One -Name 'voice studio' -Dir (Join-Path $root 'tools\voice-studio')
Build-One -Name 'pet'          -Dir $root

# The studio must sit beside the pet, because that is the first place the launcher
# looks (see app_voice_studio.go's studioCandidates). Building them apart and
# forgetting this copy is the difference between the button working and it
# reporting "找不到配音工坊程序".
$studioExe = Join-Path $root 'tools\voice-studio\build\bin\PetPCVoiceStudio.exe'
$petBin = Join-Path $root 'build\bin'
if (-not (Test-Path $studioExe)) { throw "studio exe missing: $studioExe" }
Copy-Item $studioExe $petBin -Force
Write-Host "`ncopied PetPCVoiceStudio.exe -> build\bin" -ForegroundColor Green

Write-Host "`n=== result ===" -ForegroundColor Cyan
Get-ChildItem $petBin -Filter *.exe | ForEach-Object {
    "{0,-24} {1,10:N0} bytes  {2}" -f $_.Name, $_.Length, $_.LastWriteTime
}

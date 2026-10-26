# Builds every PetPC program.
#
# There are TWO separate Wails applications in one repository now, so building them is
# two commands in the right order - which is exactly the kind of thing that gets
# half-done by hand. The market is built FIRST and its executable copied next to the
# pet's, because the pet's 「打开插件市场」 button looks for it there.
#
# There used to be a third: the standalone voice studio. Speech is a plugin now
# (market/plugins/com.shixiansi.plugin-voice), and its workshop is hosted by the market
# program's own window - so the studio is gone rather than merely unbuilt, and this
# script no longer knows about it. The stale PetPCVoiceStudio.exe is deleted from
# build\bin below, because a leftover copy would be an executable for a feature that no
# longer exists.
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
    # The market's interface IS the control center (tools/plugin-market/cc). Its build
    # output is copied over frontend/dist because THAT is the tree the Go binary embeds
    # (`//go:embed all:frontend/dist`); the Svelte app builds to its own directory, and an
    # embed of that directory would demand a dist tree that is not committed.
    #
    # The ORDER is the whole point: build cc, replace the dist, and only then run
    # wails build. Copying afterwards would embed the previous interface and look exactly
    # like "the change did nothing" - which this project has now been bitten by three
    # times (AGENTS.md §7).
    Write-Host "=== frontend: control center ===" -ForegroundColor Cyan
    Build-Frontend (Join-Path $root 'tools\plugin-market\cc')
    $ccDist = Join-Path $root 'tools\plugin-market\cc\dist'
    $embedDist = Join-Path $root 'tools\plugin-market\frontend\dist'
    if (-not (Test-Path (Join-Path $ccDist 'index.html'))) {
        throw "the control center's build produced no index.html: $ccDist"
    }
    if (Test-Path $embedDist) { Remove-Item -Recurse -Force $embedDist }
    Copy-Item -Recurse -Force $ccDist $embedDist
    Write-Host "copied the control center's build into frontend\dist (the tree the embed reads)" -ForegroundColor DarkGray
    Write-Host "=== frontend: pet ===" -ForegroundColor Cyan
    Build-Frontend (Join-Path $root 'frontend')
}

Build-One -Name 'plugin market' -Dir (Join-Path $root 'tools\plugin-market')
Build-One -Name 'pet'           -Dir $root

# The market must sit beside the pet, because that is the first place the launcher
# looks (see app_plugin_market.go's PluginMarketCandidates). Building them apart and
# forgetting this copy is the difference between the button working and it reporting
# "找不到插件市场程序".
$petBin = Join-Path $root 'build\bin'
$marketExe = Join-Path $root 'tools\plugin-market\build\bin\PetPCPluginMarket.exe'
if (-not (Test-Path $marketExe)) { throw "market exe missing: $marketExe" }
Copy-Item $marketExe $petBin -Force
Write-Host "copied PetPCPluginMarket.exe -> build\bin" -ForegroundColor Green

# The retired studio's executable is REMOVED rather than left behind. A stale binary
# beside the pet is worse than clutter: it is a program the user can still run, which
# would write the legacy library format that nothing reads any more.
$staleStudio = Join-Path $petBin 'PetPCVoiceStudio.exe'
if (Test-Path $staleStudio) {
    Remove-Item $staleStudio -Force
    Write-Host "removed the retired PetPCVoiceStudio.exe from build\bin" -ForegroundColor Yellow
}
# The studio's own build directory, for the same reason.
$staleStudioDir = Join-Path $root 'tools\voice-studio\build'
if (Test-Path $staleStudioDir) {
    Remove-Item $staleStudioDir -Recurse -Force
    Write-Host "removed tools\voice-studio\build" -ForegroundColor Yellow
}

Write-Host "`n=== result ===" -ForegroundColor Cyan
Get-ChildItem $petBin -Filter *.exe | ForEach-Object {
    "{0,-26} {1,10:N0} bytes  {2}" -f $_.Name, $_.Length, $_.LastWriteTime
}

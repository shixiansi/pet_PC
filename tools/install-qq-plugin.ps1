# Installs the QQ chat plugin (and the 1.4.0 conversation plugin it borrows its
# model settings from) straight into the pet's data directory.
#
# WHY THIS EXISTS AT ALL, given the market program installs plugins properly:
#
# the market reads a CATALOGUE, and the catalogue it is configured with is the
# published one (gh-proxy -> shixiansi/pet-pc-plugins). Packaging this repo updates
# market/index.json, market/dist and the built-in sample source, but publishing the
# separate market repository is a push this machine cannot do. So a freshly built
# package is invisible to a market whose address still points at the published one -
# which reads exactly like "the package was never built".
#
# This script is the local equivalent of clicking 安装/更新 in the market: it expands
# the zip the packager produced into <dataDir>\plugins\<id>, and records the row in
# plugins.json. It GRANTS the permissions the package declares, which is what the
# market's install flow does after showing them - so it is a development shortcut,
# not a way to skip a consent screen you never saw.
#
# NOTHING is written outside the data directory and tools/: the pet's own config is
# untouched, the plugin's private data (its saved settings, the DPAPI-encrypted API
# key, its per-group memory) is kept, and an uninstall is a directory delete.
#
# Usage:
#   pwsh -File tools/install-qq-plugin.ps1
#   pwsh -File tools/install-qq-plugin.ps1 -DataDir D:\PetPCData -UpdateMarketBase
[CmdletBinding()]
param(
    # Where the packaged zips live. Defaults to the packager's output directory.
    [string]$ZipDir = (Join-Path (Split-Path -Parent $PSScriptRoot) 'market\dist'),
    # The pet's data directory (store.DefaultDir()).
    [string]$DataDir = (Join-Path $env:APPDATA 'PetPC'),
    # Point the market program at the BUILT-IN source (empty address = "use the
    # sample source embedded in the binary"), so the newly packaged versions show up
    # in its list. Off by default: overwriting a user's market address is a decision.
    [switch]$UpdateMarketBase
)

$ErrorActionPreference = 'Stop'

# The two packages: the conversation plugin is UPDATED (the QQ plugin borrows its
# model settings, and only 1.4.0 declares shareLLM), the QQ plugin is INSTALLED.
$packages = @(
    @{ Id = 'com.shixiansi.plugin-chat'; Zip = 'com.shixiansi.plugin-chat-1.4.0.zip'; Version = '1.7.4'; Update = $true },
    @{ Id = 'com.shixiansi.plugin-qq';   Zip = 'com.shixiansi.plugin-qq-1.9.0.zip';   Version = '1.9.0'; Update = $false }
)

if (-not (Test-Path -LiteralPath $DataDir)) {
    throw "数据目录不存在：$DataDir（先启动一次桌宠）"
}
$pluginsDir = Join-Path $DataDir 'plugins'
$indexPath = Join-Path $DataDir 'plugins.json'
New-Item -ItemType Directory -Force -Path $pluginsDir | Out-Null

# --- load the installed index -------------------------------------------------
#
# An unreadable index is rebuilt rather than refused: it holds nothing the packages
# themselves do not restate, unlike the private data this script must never drop.
$index = $null
if (Test-Path -LiteralPath $indexPath) {
    try { $index = Get-Content -LiteralPath $indexPath -Raw | ConvertFrom-Json } catch { $index = $null }
}
if ($null -eq $index) {
    $index = [pscustomobject]@{ version = 1; plugins = @() }
}
$rows = @($index.plugins)

# --- expand + record ----------------------------------------------------------
$summary = @()
foreach ($p in $packages) {
    $zipPath = Join-Path $ZipDir $p.Zip
    if (-not (Test-Path -LiteralPath $zipPath)) {
        throw "找不到安装包：$zipPath（先跑 pwsh -File market/tools/package.ps1）"
    }

    $target = Join-Path $pluginsDir $p.Id
    $staging = Join-Path $DataDir ('plugins-tmp\' + $p.Id + '-' + [guid]::NewGuid().ToString('N').Substring(0, 8))
    New-Item -ItemType Directory -Force -Path $staging | Out-Null
    try {
        Expand-Archive -LiteralPath $zipPath -DestinationPath $staging -Force
        # The package's own manifest is authoritative, exactly as the installer
        # treats it: the permission list recorded below is the one INSIDE the zip.
        $manifestPath = Join-Path $staging 'manifest.json'
        if (-not (Test-Path -LiteralPath $manifestPath)) { throw "$($p.Zip) 里没有 manifest.json" }
        $manifest = Get-Content -LiteralPath $manifestPath -Raw | ConvertFrom-Json

        if (Test-Path -LiteralPath $target) { Remove-Item -LiteralPath $target -Recurse -Force }
        Move-Item -LiteralPath $staging -Destination $target
    } finally {
        if (Test-Path -LiteralPath $staging) { Remove-Item -LiteralPath $staging -Recurse -Force }
    }

    $declared = @($manifest.permissions)
    $existing = $rows | Where-Object { $_.id -eq $p.Id } | Select-Object -First 1
    if ($existing) {
        # An update keeps what the user already allowed and when they installed it:
        # re-granting everything on every update is how a permission the user
        # deliberately unticked would come back by itself.
        $existing.version = $p.Version
        $existing.enabled = $true
        if (@($existing.granted).Count -eq 0) { $existing.granted = $declared }
        # A permission added by the new manifest still needs granting; the market
        # shows it as "待授权" and this records it, because the plugin cannot work
        # without the ones it added (shareLLM needs nothing new, net.ws is new only
        # for a plugin installed before this change).
        $existing.granted = @(@($existing.granted) + @($declared | Where-Object { @($existing.granted) -notcontains $_ }) | Select-Object -Unique)
    } else {
        $row = [pscustomobject]@{
            id          = $p.Id
            version     = $p.Version
            enabled     = $true
            granted     = $declared
            installedAt = (Get-Date).ToString('o')
        }
        $rows = @($rows) + $row
    }
    $summary += [pscustomobject]@{
        Plugin   = $p.Id
        Version  = $p.Version
        Action   = if ($p.Update) { '更新' } else { '安装' }
        Granted  = ($declared -join ', ')
    }
}

# --- write the index atomically ----------------------------------------------
#
# temp file in the SAME directory, then rename: the pet is a second process reading
# this file, and a half-written index would look like "no plugins installed".
$index.plugins = $rows
$tmp = Join-Path $DataDir ('plugins.json.tmp-' + [guid]::NewGuid().ToString('N').Substring(0, 8))
$index | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath $tmp -Encoding UTF8
Move-Item -LiteralPath $tmp -Destination $indexPath -Force

# --- optionally point the market at the built-in source ----------------------
if ($UpdateMarketBase) {
    $marketCfg = Join-Path $DataDir 'plugin-market.json'
    $cfg = $null
    if (Test-Path -LiteralPath $marketCfg) {
        try { $cfg = Get-Content -LiteralPath $marketCfg -Raw | ConvertFrom-Json } catch { $cfg = $null }
    }
    if ($null -eq $cfg) { $cfg = [pscustomobject]@{ marketBaseUrl = ''; lastSelectedId = ''; windowW = 980; windowH = 700 } }
    $cfg.marketBaseUrl = ''
    $tmpCfg = Join-Path $DataDir ('plugin-market.json.tmp-' + [guid]::NewGuid().ToString('N').Substring(0, 8))
    $cfg | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath $tmpCfg -Encoding UTF8
    Move-Item -LiteralPath $tmpCfg -Destination $marketCfg -Force
    Write-Host "市场地址已清空：市场程序将使用内置示例源（含 QQ 插件与对话插件 1.4.0）。" -ForegroundColor Yellow
}

$summary | Format-Table -AutoSize
Write-Host "已写入：$indexPath" -ForegroundColor Green
Write-Host "重启桌宠（或打开一次面板）后生效；QQ 插件默认只回应 @，地址 127.0.0.1:3000。" -ForegroundColor Green

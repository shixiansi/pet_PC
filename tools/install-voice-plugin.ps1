# 把「语音插件」装进本机已装目录，并让它接管旧的配音库。
#
# 为什么需要这一步：**语音插件是这一版新增的**，而你机器上还没有它。桌宠自己不再
# 有语音功能了（台词、音色、开关、音频全归插件），所以「桌宠会不会说话」现在取决于
# 这个插件装没装。
#
# 这个脚本做三件事：
#
#   1. 从仓库把插件的 4 个资源文件（manifest.json / panel.html / app.js / style.css）
#      和 5 个内置音频（lines\*.wav）复制进 `plugins\com.shixiansi.plugin-voice\`；
#   2. 在 `plugins.json` 里登记它，并**授权它声明的 5 项权限**
#      （audio / events / storage / plugin.config / net.fetch）——不授权的话，
#      桌宠启动时会明确拒绝它，插件装了也一声不响；
#   3. 建好它的私有目录，好让桌宠启动时的一次性迁移有地方放东西。
#
# 迁移本身不在这里做：它由桌宠在启动时执行（`plugins.MigrateVoiceData`），
# 只**读**你原来的 `voice\user\`，把 12 段音频复制进插件私有目录，
# 然后写一个 `.migrated` 标记。原文件一个都不动，那就是回滚路径。
#
# 用法：
#
#   powershell -NoProfile -ExecutionPolicy Bypass -File tools\install-voice-plugin.ps1

$ErrorActionPreference = 'Stop'

$dataDir = if ($env:PETPC_DATA_DIR) { $env:PETPC_DATA_DIR } else { Join-Path $env:APPDATA 'PetPC' }
$repoRoot = Split-Path -Parent $PSScriptRoot
$src = Join-Path $repoRoot 'market\plugins\com.shixiansi.plugin-voice'
$pluginId = 'com.shixiansi.plugin-voice'
$dst = Join-Path $dataDir "plugins\$pluginId"
$privateDir = Join-Path $dataDir "plugin-data\$pluginId"
$indexPath = Join-Path $dataDir 'plugins.json'

Write-Host "源：  $src"
Write-Host "目标：$dst"
Write-Host ""

if (-not (Test-Path $src)) {
    Write-Host "找不到插件源目录，先确认仓库路径。" -ForegroundColor Red
    exit 1
}

# 安装前先自己校验一遍这份包是完整的。装一个缺音频的插件，症状是"装好了但不出声"，
# 那比装不上更难查。
$manifestPath = Join-Path $src 'manifest.json'
$manifest = Get-Content $manifestPath -Raw | ConvertFrom-Json

if ($manifest.id -ne $pluginId) {
    Write-Host "manifest 的 id 是 $($manifest.id)，期望 $pluginId。" -ForegroundColor Red
    exit 1
}

$assets = @($manifest.assets)
if ($assets.Count -eq 0) {
    Write-Host "manifest 没有声明任何 assets，内置音频会缺失。" -ForegroundColor Red
    exit 1
}
foreach ($a in $assets) {
    $p = Join-Path $src ($a -replace '/', '\')
    if (-not (Test-Path $p -PathType Leaf)) {
        Write-Host "manifest 声明的资源不存在：$a" -ForegroundColor Red
        exit 1
    }
    $bytes = [System.IO.File]::ReadAllBytes($p)
    if ($bytes.Length -lt 1000 -or [System.Text.Encoding]::ASCII.GetString($bytes, 0, 4) -ne 'RIFF') {
        Write-Host "资源不是有效的 WAV：$a（$($bytes.Length) 字节）" -ForegroundColor Red
        exit 1
    }
}
Write-Host "包校验通过：$($assets.Count) 个内置音频都是有效的 RIFF/WAVE。" -ForegroundColor Green

# 1. 复制文件。先清空目标目录里的资源，避免上一版的残留（比如改名后的旧 js）继续被加载。
New-Item -ItemType Directory -Path $dst -Force | Out-Null
foreach ($name in @('panel.html', 'app.js', 'style.css')) {
    $target = Join-Path $dst $name
    if (Test-Path $target) { Remove-Item $target -Force }
}
Copy-Item (Join-Path $src 'manifest.json') $dst -Force
foreach ($name in @('panel.html', 'app.js', 'style.css')) {
    Copy-Item (Join-Path $src $name) $dst -Force
}
# lines\ 整目录复制，并把旧的多余文件清掉：多了不会出错，但会让下一次核对困惑。
$linesDst = Join-Path $dst 'lines'
if (Test-Path $linesDst) { Remove-Item $linesDst -Recurse -Force }
New-Item -ItemType Directory -Path $linesDst -Force | Out-Null
Copy-Item (Join-Path $src 'lines\*.wav') $linesDst -Force

Write-Host "已复制资源文件与 $((Get-ChildItem $linesDst).Count) 个音频。" -ForegroundColor Green

# 2. 登记并授权。
if (-not (Test-Path $indexPath)) {
    Write-Host "找不到 plugins.json（$indexPath）。先启动一次桌宠，或先在插件市场里装一个插件。" -ForegroundColor Red
    exit 1
}
$index = Get-Content $indexPath -Raw | ConvertFrom-Json

# 授权它声明的一切。**这是刻意的**：插件没被授权时，桌宠会明确拒绝它的每一次调用，
# 而症状就是"插件装了但完全没反应"——比装不上更难判断。
$granted = @($manifest.permissions)

$existing = @($index.plugins | Where-Object { $_.id -eq $pluginId })
if ($existing.Count -gt 0) {
    $existing[0].version = $manifest.version
    $existing[0].enabled = $true
    $existing[0].granted = $granted
    Write-Host "已更新 plugins.json 里的登记（version $($manifest.version)，授权 $($granted.Count) 项）。" -ForegroundColor Green
} else {
    $entry = [pscustomobject]@{
        id          = $pluginId
        version     = $manifest.version
        enabled     = $true
        granted     = $granted
        installedAt = (Get-Date).ToString('o')
    }
    $index.plugins = @($index.plugins) + $entry
    Write-Host "已在 plugins.json 里新增登记（授权 $($granted.Count) 项）。" -ForegroundColor Green
}

# 写回时保持缩进与中文可读：ConvertTo-Json 在 5.1 上会把非 ASCII 转成 \uXXXX。
$json = $index | ConvertTo-Json -Depth 8
$json = [regex]::Replace($json, '\\u([0-9a-fA-F]{4})', {
    param($m) [char][int]("0x" + $m.Groups[1].Value)
})
$utf8 = New-Object System.Text.UTF8Encoding($false)
[System.IO.File]::WriteAllText($indexPath, $json, $utf8)

# 3. 建好私有目录，让桌宠启动时的迁移有地方写。
New-Item -ItemType Directory -Path $privateDir -Force | Out-Null
$audioDir = Join-Path $privateDir 'audio'
New-Item -ItemType Directory -Path $audioDir -Force | Out-Null

# 旧的配音库还在不在？在的话告诉用户迁移会在下次启动发生。
$legacy = Join-Path $dataDir 'voice\user\voice-user.json'
if (Test-Path $legacy) {
    $wavs = (Get-ChildItem (Join-Path $dataDir 'voice\user\clips') -Recurse -File -Filter *.wav -ErrorAction SilentlyContinue | Measure-Object).Count
    Write-Host ""
    Write-Host "找到旧配音库（$wavs 段音频）。" -ForegroundColor Cyan
    Write-Host "下次启动桌宠时会把它们**复制**进插件私有目录，原文件不动。" -ForegroundColor Cyan
} else {
    Write-Host ""
    Write-Host "没有找到旧配音库，插件会用自带的 5 段内置音频。" -ForegroundColor Cyan
}

Write-Host ""
Write-Host "完成。现在启动 build\bin\PetPC.exe 即可。" -ForegroundColor Green
Write-Host "插件的数据在：$privateDir"

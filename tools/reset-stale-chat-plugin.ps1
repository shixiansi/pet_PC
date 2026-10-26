# 把桌宠数据目录里那个过期的「对话插件 1.0.0」清出来，让你能从插件市场重新安装 1.1.0。
#
# 为什么需要这一步：
#
#   你机器上装的 com.shixiansi.plugin-chat 是 1.0.0。它的 manifest 里既没有
#   `panelSlot: "chat"`，也没有 `config` 声明，而这两样东西是 1.1.0 才引入的。
#   于是出现「明明装了插件，面板却说没装、市场里也没有模型配置」这同一个根因的两个症状：
#
#     * 面板「对话」槽位的解析规则是「已装 + 已启用 + 有 pet.surface + panelSlot=="chat"」，
#       1.0.0 不满足最后一条，所以槽位判定为空；
#     * 市场的配置区渲染的是 manifest 的 `config` 数组，1.0.0 没有这个字段，
#       所以显示「这个插件没有声明任何配置项」。
#
# 为什么不能「直接在市场里更新」：
#
#   旧的 1.0.0 没有 `panelSlot`，所以它根本不会出现在「可更新」的判断里——
#   判断更新需要 id 能对上。这一版新增了「更新」按钮（不再强制先卸载），
#   但要让 1.0.0 走到 1.1.0，仍需要先把这份过期安装清掉一次。
#
# 这个脚本做什么：
#
#   1. 把 plugins/<id>/ 改名为 plugins/<id>.old-<时间戳>（保留，不删）；
#   2. 把 plugins.json 备份成 plugins.json.bak-<时间戳>，然后重置为空列表；
#   3. 把市场地址**清空**，强制市场程序使用内嵌示例源（-KeepMarketURL 可跳过）；
#   4. **不动** plugin-data/<id>/，你的插件私有数据（迁移过来的对话历史、
#      用量与密钥）原样保留，1.1.0 装好后仍会读到它们。
#
# 关于第 3 步（关键，别跳过）：
#
#   内嵌示例源随 exe 分发，它已经是 1.1.0，而且 sha256 与包字节一致。
#   而远端仓库（`pet-pc-plugins` 的 `main`）**至今仍是 1.0.0**：
#   它的 index.json 还写着 `"version": "1.0.0"` 和 `...-1.0.0.zip`。
#   所以只要市场在读远端，重新安装拿到的还是那个没有 `panelSlot`、
#   没有 `config` 的旧包，症状一模一样。清空地址就是让它改走内嵌源，
#   从而真正拿到 1.1.0。
#
#   远端要跟上，需要你把 `market/` 目录里的改动提交并推送（说明见文件末尾）。
#
# 用法：
#
#   powershell -NoProfile -ExecutionPolicy Bypass -File tools\reset-stale-chat-plugin.ps1
#   powershell -NoProfile -ExecutionPolicy Bypass -File tools\reset-stale-chat-plugin.ps1 -KeepMarketURL
#
# 跑完之后：打开插件市场 → 选中「对话插件」→ 安装并授权（勾上所需权限）→
# 回到桌宠面板的「对话」tab，界面与「配置」区都会出现。

param(
    # 保留现有的市场地址（等远端仓库更新到 1.1.0 之后再用）。
    [switch]$KeepMarketURL
)

$ErrorActionPreference = 'Stop'

$dataDir = if ($env:PETPC_DATA_DIR) { $env:PETPC_DATA_DIR } else { Join-Path $env:APPDATA 'PetPC' }
if (-not (Test-Path $dataDir)) {
    Write-Host "找不到数据目录：$dataDir" -ForegroundColor Red
    exit 1
}

Write-Host "数据目录：$dataDir" -ForegroundColor Cyan

$pluginId = 'com.shixiansi.plugin-chat'
$pluginDir = Join-Path $dataDir "plugins\$pluginId"
$indexPath = Join-Path $dataDir 'plugins.json'
$stamp = Get-Date -Format 'yyyyMMdd-HHmmss'

# 先看清楚现在是什么状态，再决定要不要重置。
#
# 三种状态要分开报，因为它们对用户的含义完全不同，而"什么都不装"绝不能被说成
# "已经是最新版" —— 那是把一个待办说成了已完成：
#
#   * 没装：索引里没有这一行。这不是"不需要重置"，而是"还没有可重置的东西"，
#     用户接下来该做的是去市场装上；
#   * 装了但不是 1.1.0：要重置，这是这个脚本存在的原因；
#   * 装了且是 1.1.0：真的不需要重置。
$installedVersion = ''
$indexParsed = $false
if (Test-Path $indexPath) {
    try {
        $index = Get-Content $indexPath -Raw -Encoding UTF8 | ConvertFrom-Json
        $indexParsed = $true
        foreach ($row in $index.plugins) {
            if ($row.id -eq $pluginId) { $installedVersion = [string]$row.version }
        }
    } catch {
        Write-Host "plugins.json 解析失败，将按「需要重置」处理：$_" -ForegroundColor Yellow
    }
}

$needsReset = $false
if (-not $indexParsed) {
    Write-Host "索引状态：无法判断（按需要重置处理）" -ForegroundColor Yellow
    $needsReset = $true
} elseif ($installedVersion -eq '') {
    Write-Host "索引状态：没有安装「$pluginId」" -ForegroundColor Yellow
} elseif ($installedVersion -eq '1.1.0') {
    Write-Host "索引状态：已装 1.1.0（不需要重置）" -ForegroundColor Green
} else {
    Write-Host "索引状态：已装 $installedVersion（需要重置）" -ForegroundColor Yellow
    $needsReset = $true
}

if ($needsReset) {
    # 1. 归档插件文件（保留，不删）。
    if (Test-Path $pluginDir) {
        $archived = "$pluginDir.old-$stamp"
        Move-Item -LiteralPath $pluginDir -Destination $archived -Force
        Write-Host "已归档插件文件 -> $(Split-Path -Leaf $archived)" -ForegroundColor Green
    }
} else {
    Write-Host "跳过插件文件归档与索引重置。" -ForegroundColor DarkGray
}

# 2. 备份并重置索引。
if (Test-Path $indexPath) {
    $backup = "$indexPath.bak-$stamp"
    Copy-Item -LiteralPath $indexPath -Destination $backup -Force
    Write-Host "已备份索引 -> $(Split-Path -Leaf $backup)" -ForegroundColor Green
    $empty = @{ version = 1; plugins = @() } | ConvertTo-Json -Depth 4
    # 不带 BOM：桌宠与市场程序都按 UTF-8 读这个文件，BOM 会被当成 JSON 的第一个字符。
    [System.IO.File]::WriteAllText($indexPath, $empty, (New-Object System.Text.UTF8Encoding($false)))
    Write-Host "已把 plugins.json 重置为空列表" -ForegroundColor Green
}

# 3. 清空市场地址，强制走内嵌示例源（它已经是 1.1.0）。
$marketCfg = Join-Path $dataDir 'plugin-market.json'
if (-not $KeepMarketURL) {
    if (Test-Path $marketCfg) {
        $mc = Get-Content $marketCfg -Raw -Encoding UTF8 | ConvertFrom-Json
        $old = $mc.marketBaseUrl
        $mc.marketBaseUrl = ''
        $json = $mc | ConvertTo-Json -Depth 4
        [System.IO.File]::WriteAllText($marketCfg, $json, (New-Object System.Text.UTF8Encoding($false)))
        Write-Host "已清空市场地址（原值：$old）" -ForegroundColor Green
        Write-Host "  -> 市场程序将改用随 exe 分发的内嵌示例源（1.1.0）" -ForegroundColor Green
    } else {
        Write-Host "没有 plugin-market.json；市场程序首次启动时会自己建一个（地址为空 = 走内嵌源）" -ForegroundColor Yellow
    }
}

# 4. 私有数据保持不动，并明确说明。
$privateDir = Join-Path $dataDir "plugin-data\$pluginId"
if (Test-Path $privateDir) {
    Write-Host "保留插件私有数据：$privateDir" -ForegroundColor Green
    Get-ChildItem $privateDir -ErrorAction SilentlyContinue | ForEach-Object {
        Write-Host "    $($_.Name)"
    }
}

Write-Host ""
Write-Host "完成。接下来：" -ForegroundColor Cyan
Write-Host "  1. 打开 build\bin\PetPCPluginMarket.exe（或桌宠设置页的「打开插件市场」）"
Write-Host "     顶部地址框应该是空的，标题旁写着「内置示例源」"
Write-Host "  2. 选中「对话插件」，点「安装并授权」，勾上权限"
Write-Host "     —— 版本应为 1.1.0；如果是 1.0.0，说明地址没清干净"
Write-Host "  3. 回到桌宠面板的「对话」tab：界面出现，右侧「配置」区可填模型与 API Key"
Write-Host ""
Write-Host "远端仓库什么时候要更新：" -ForegroundColor Cyan
Write-Host "  内嵌源只能救这一台机器。要让别人（和你换机后）也拿到 1.1.0，"
Write-Host "  需要把 market\ 目录的改动提交并推送："
Write-Host ""
Write-Host "    cd market"
Write-Host "    git add -A"
Write-Host '    git commit -m "feat: 对话插件 1.1.0（panelSlot + config 声明 + 5 块浮层矩形）"'
Write-Host "    git push origin main"
Write-Host ""
Write-Host "  推送后可以改回地址：在市场程序顶部的地址框里填回"
Write-Host "  https://gh-proxy.com/https://raw.githubusercontent.com/shixiansi/pet-pc-plugins/main"
Write-Host "  再点「保存」。"

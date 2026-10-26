# 把修好的「对话插件」文件装进已装目录。
#
# 为什么需要这一步：插件市场是**按 zip 里的字节**安装的，而你机器上装的那份是
# 修复前的旧字节。重新安装一次也能修好，但那会经过下载/校验/解压那条路；这个脚本
# 直接把仓库里已经修好的 4 个资源文件复制过去，然后**重算 sha256 无关**——
# 已装状态由 plugins.json 记录，它只关心 id/version/enabled/granted，不校验文件摘要。
#
# 修的是什么：插件样式表里 `#dots` 是 `position:absolute; inset:0; display:flex`，
# 而它在 panel 视图下被标了 `hidden`。浏览器对 `[hidden]` 的 `display:none` 在
# 用户代理样式表里，任何作者样式里的 `display` 都会盖过它 —— 于是那三个点变成一个
# 铺满整个面板的**透明层**，盖住了输入框和四个按钮。界面看着完全正常，点击却全被
# 那一层吃掉，所以"发送/停止/气泡/浮层面"全都点不动。
#
# 修法是给样式表加一条权威规则：
#
#     [hidden] { display: none !important; }
#
# 用法：
#
#   powershell -NoProfile -ExecutionPolicy Bypass -File tools\install-fixed-chat-plugin.ps1

$ErrorActionPreference = 'Stop'

$dataDir = if ($env:PETPC_DATA_DIR) { $env:PETPC_DATA_DIR } else { Join-Path $env:APPDATA 'PetPC' }
$repoRoot = Split-Path -Parent $PSScriptRoot
$src = Join-Path $repoRoot 'market\plugins\com.shixiansi.plugin-chat'
$dst = Join-Path $dataDir 'plugins\com.shixiansi.plugin-chat'
$indexPath = Join-Path $dataDir 'plugins.json'

Write-Host "源：$src"
Write-Host "目标：$dst"
Write-Host ""

if (-not (Test-Path $src)) {
    Write-Host "找不到源目录，先确认仓库路径。" -ForegroundColor Red
    exit 1
}
if (-not (Test-Path $dst)) {
    Write-Host "还没有安装这个插件。请先在插件市场里安装它，再跑这个脚本。" -ForegroundColor Yellow
    exit 1
}

# 先确认要装的这份确实是修好的，避免复制一份仍然有 bug 的文件。
$fixedCss = Get-Content (Join-Path $src 'style.css') -Raw
if ($fixedCss -notmatch '\[hidden\]\s*\{[^}]*display:\s*none\s*!important') {
    Write-Host "源样式表里没有 [hidden] 权威规则，这不是修好的版本。" -ForegroundColor Red
    exit 1
}
Write-Host "源样式表已包含 [hidden] 权威规则。" -ForegroundColor Green

# 只复制资源文件。manifest.json 也一起，因为内容与已装版本一致（1.1.0），
# 复制它不会改变任何已授权状态。
$files = @('manifest.json', 'panel.html', 'app.js', 'style.css')
foreach ($f in $files) {
    $from = Join-Path $src $f
    if (-not (Test-Path $from)) {
        Write-Host "源里缺少 $f" -ForegroundColor Red
        exit 1
    }
    Copy-Item -LiteralPath $from -Destination (Join-Path $dst $f) -Force
    Write-Host "  已更新 $f" -ForegroundColor Green
}

# 报告已装状态，便于确认没有被动过。
if (Test-Path $indexPath) {
    $index = Get-Content $indexPath -Raw -Encoding UTF8 | ConvertFrom-Json
    foreach ($row in $index.plugins) {
        if ($row.id -eq 'com.shixiansi.plugin-chat') {
            Write-Host ""
            $granted = ($row.granted -join ',')
            Write-Host "已装状态：version=$($row.version) enabled=$($row.enabled) granted=$granted" -ForegroundColor Cyan
        }
    }
}

Write-Host ""
Write-Host "完成。接下来：" -ForegroundColor Cyan
Write-Host "  1. 完全退出桌宠（托盘菜单或任务管理器），再重新启动 build\bin\PetPC.exe"
Write-Host "     —— 插件文件是按 no-store 提供的，但页面本身要重新加载才会读新的 style.css"
Write-Host "  2. 打开面板的「对话」tab"
Write-Host "  3. 现在输入框和「发送/停止/气泡/浮层面」应该都能点"

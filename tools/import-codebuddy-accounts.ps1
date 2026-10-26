#Requires -Version 7.0
<#
.SYNOPSIS
  把 CodexSwitch「CodeBuddy 多账号」快照库（~/.dsh/codebuddy-accounts/）里的账号，
  导入 dsh-our-free-model 的 channel-pack 白嫖账号池。

.DESCRIPTION
  两条链路本来互不相通：

    * @shatyuka/dsh-llm-codebuddy  —— 单账号，凭据写 ~/.dsh/codebuddy-auth.json，
      每次登录直接覆盖；CodexSwitch 的 codebuddy-accounts 插件在它之外维护快照库，
      靠「切换」原子覆写那一个文件。
    * dsh-our-free-model / channel-pack —— 天生的多账号池：账号索引在
      channel-pack/state.json，凭据本体在 .credentials.yaml 的 refs: 段。
      同一个 provider 下可以挂任意多个账号，按数组顺序轮换。

  所以接入多个账号**不需要切换**，只要把快照转换成池子的凭据形状、追加进那两个文件。

  三个必须处理的细节（脚本已实现）：

    1. 字段名不同。快照是 { auth.accessToken, ..., account.uid }，池子是
       { access_token, refresh_token, expires_at, domain, user_id, ... }。
    2. refresh_token 必须是非空字符串：池子的 isRefreshable() 直接取 .length，
       没有兜底，缺了会抛异常。脚本会校验并跳过不合格的快照。
    3. 按 domain 分渠道。buddy / workbuddy 是两个独立产品，凭据互不可见；
       而且启动时会执行 pruneAccountsWithForeignDomain(WORKBUDDY)，
       domain 不是 www.workbuddy.ai 的 workbuddy 账号会被**直接删除**并提示重新登录。

  安全策略：先备份两个目标文件；写入走临时文件 + 替换（原子）；默认只预演，
  加 -Apply 才落盘；检测到 DSH 在运行会拒绝写入（内存里的账号表会把磁盘改动覆盖掉）。

.PARAMETER DshHome
  harness home。默认 $env:DSH_HOME，否则 ~/.dsh。

.PARAMETER SnapshotDir
  快照库目录。默认 <DshHome>/codebuddy-accounts。

.PARAMETER Apply
  真正写盘。不加时只打印将要写入的内容（预演）。

.PARAMETER Force
  DSH 正在运行时也强制写入。不推荐：运行中的进程可能在之后把内存里的旧账号表
  写回，覆盖掉本次导入。

.EXAMPLE
  # 1) 先预演，看看要导入哪些、写成什么
  pwsh -File .\import-codebuddy-accounts.ps1

.EXAMPLE
  # 2) 关掉 DSH，确认它真的退出了，再落盘
  pwsh -File .\import-codebuddy-accounts.ps1 -Apply
#>
[CmdletBinding()]
param(
    [string]$DshHome,
    [string]$SnapshotDir,
    [switch]$Apply,
    [switch]$Force
)

$ErrorActionPreference = 'Stop'

# --- 路径解析 ---------------------------------------------------------------

if (-not $DshHome) {
    $DshHome = if ($env:DSH_HOME) { $env:DSH_HOME } else { Join-Path $HOME '.dsh' }
}
if (-not $SnapshotDir) { $SnapshotDir = Join-Path $DshHome 'codebuddy-accounts' }

$credsPath = Join-Path $DshHome '.credentials.yaml'
$statePath = Join-Path $DshHome 'channel-pack' 'state.json'

foreach ($p in @($credsPath, $statePath)) {
    if (-not (Test-Path -LiteralPath $p)) { throw "找不到目标文件：$p" }
}
if (-not (Test-Path -LiteralPath $SnapshotDir)) { throw "找不到快照库：$SnapshotDir" }

Write-Host "DSH home   : $DshHome"
Write-Host "快照库     : $SnapshotDir"
Write-Host "凭据文件   : $credsPath"
Write-Host "账号索引   : $statePath"
Write-Host ""

# --- 运行中检测 -------------------------------------------------------------
# channel-pack 把账号表缓存在内存（FileStore.cache），writeAccounts() 会把内存里的
# 整表写回磁盘。DSH 还在跑的话，我们写进去的账号可能被后续任意一次账号写入覆盖。
#
# 三重探测，因为单一手段在受限环境里会**静默失效**：实测本机沙箱下
# Get-CimInstance 抛「拒绝访问」、Get-Process -Name node 返回空，只有 netstat
# 还能看到监听端口。任何一种报出「在跑」就认定在跑，避免保护形同虚设。

function Get-ListeningDshPid {
    # DSH 的 Web GUI 默认监听 127.0.0.1:3080；端口被占即视为在运行。
    try {
        $rows = netstat -ano | Select-String ':3080\s' | Select-String 'LISTENING'
        foreach ($row in $rows) {
            $parts = ($row.ToString() -split '\s+') | Where-Object { $_ -ne '' }
            if ($parts.Length -ge 5) { return [int]$parts[-1] }
        }
    }
    catch { }
    return $null
}

function Get-RunningDsh {
    $found = [System.Collections.Generic.List[object]]::new()

    # 1) 端口：最稳，沙箱里也能用
    $portPid = Get-ListeningDshPid
    if ($portPid) {
        $found.Add([pscustomobject]@{ Pid = $portPid; How = '监听 127.0.0.1:3080' })
    }

    # 2) 进程名：受限环境下可能被拒，失败就跳过
    try {
        Get-Process -ErrorAction Stop |
            Where-Object { $_.ProcessName -match '^(node|dsh)' } |
            ForEach-Object { $found.Add([pscustomobject]@{ Pid = $_.Id; How = "进程名 $($_.ProcessName)" }) }
    }
    catch { }

    # 3) 命令行：最能定性，但同样可能被拒
    try {
        Get-CimInstance Win32_Process -Filter "Name = 'node.exe'" -ErrorAction Stop |
            Where-Object { $_.CommandLine -and $_.CommandLine -match 'dsh' } |
            ForEach-Object { $found.Add([pscustomobject]@{ Pid = $_.ProcessId; How = '命令行含 dsh' }) }
    }
    catch { }

    # 按 pid 去重
    $seen = @{}
    $unique = [System.Collections.Generic.List[object]]::new()
    foreach ($item in $found) {
        if ($seen.ContainsKey($item.Pid)) { continue }
        $seen[$item.Pid] = $true
        $unique.Add($item)
    }
    return $unique
}

$runningDsh = @(Get-RunningDsh)
if ($runningDsh.Count -gt 0) {
    Write-Host "检测到 DSH 正在运行（$($runningDsh.Count) 个迹象）：" -ForegroundColor Yellow
    foreach ($proc in $runningDsh) {
        Write-Host "  pid $($proc.Pid) - $($proc.How)"
    }
    if ($Apply -and -not $Force) {
        throw "DSH 正在运行。先完全退出它再运行本脚本（或加 -Force 自行承担被覆盖的风险）。"
    }
    if (-not $Apply) {
        Write-Host "（这是预演，不落盘；真正写入前请先退出 DSH。）" -ForegroundColor Yellow
    }
    Write-Host ""
}
else {
    Write-Host "未检测到运行中的 DSH（三种探测都没报）。" -ForegroundColor DarkGray
    Write-Host ""
}

# --- 渠道映射 ---------------------------------------------------------------
# 只有这两个 provider 的凭据形状是同一套（BuddyAuth）；domain 决定归属。

$DomainToProvider = @{
    'www.codebuddy.cn'    = 'buddy'      # 国内版（实测凭据里的 domain）
    'copilot.tencent.com' = 'buddy'      # 产品配置的 apiDomain
    'www.workbuddy.ai'    = 'workbuddy'  # 国际版
}

# --- 读取现有池子 -----------------------------------------------------------

$credsLines = @([System.IO.File]::ReadAllLines($credsPath))

$refsIdx = -1
for ($i = 0; $i -lt $credsLines.Count; $i++) {
    if ($credsLines[$i] -match '^refs:\s*$') { $refsIdx = $i; break }
}
if ($refsIdx -lt 0) { throw "在 $credsPath 里找不到 refs: 段。" }

# refs 段的结束位置：之后第一条顶格的（非缩进）行
$refsEnd = $credsLines.Count
for ($i = $refsIdx + 1; $i -lt $credsLines.Count; $i++) {
    if ($credsLines[$i] -match '^\S') { $refsEnd = $i; break }
}

# 提取一个凭据值里的 user_id，用来判断某账号是否已在池中。
#
# 这里**不能**只解析键所在那一行：DSH 自己写进去的凭据值可能是跨行的流式 JSON
# （实测 BUDDY_ACCOUNT_96B477EC 的 "user_id" 落在第 18 行，而键在第 16 行）。
# 只取键行会让 JSON 解析失败、user_id 读成空，去重整个失效 —— 于是池里已有的账号
# 会被重复导入一份旧令牌，之后再被轮询选中就会 401。故按「键行 + 其后续缩进行」
# 整体拼接，再用正则提取，跨行也稳。
function Get-CredentialUserId([string]$raw) {
    $m = [regex]::Match($raw, '"user_id"\s*:\s*"([^"]*)"')
    if ($m.Success) { return $m.Groups[1].Value }
    return ''
}

$existingRefs = @{}   # ref 名 -> user_id（提取不到则为 ''）
$curName = $null
$curBuf = [System.Collections.Generic.List[string]]::new()
for ($i = $refsIdx + 1; $i -lt $refsEnd; $i++) {
    if ($credsLines[$i] -match '^\s+([A-Za-z][A-Za-z0-9_]*):\s?(.*)$') {
        if ($curName) { $existingRefs[$curName] = (Get-CredentialUserId ($curBuf -join "`n")) }
        $curName = $Matches[1]
        $curBuf = [System.Collections.Generic.List[string]]::new()
        $curBuf.Add($Matches[2])
    }
    elseif ($curName) {
        # 属于上一个键的跨行值，继续累积
        $curBuf.Add($credsLines[$i].Trim())
    }
}
if ($curName) { $existingRefs[$curName] = (Get-CredentialUserId ($curBuf -join "`n")) }

$existingUids = @{}
foreach ($pair in $existingRefs.GetEnumerator()) {
    if ($pair.Value) { $existingUids[$pair.Value] = $pair.Key }
}

$stateRaw = [System.IO.File]::ReadAllText($statePath)
$state = $stateRaw | ConvertFrom-Json
# ConvertFrom-Json 可能把单元素数组解包成标量，统一裹成数组再处理。
$stateAccounts = @($state.accounts)

Write-Host "池内现有账号 $($stateAccounts.Count) 个："
foreach ($acc in $stateAccounts) {
    $uid = $existingRefs[$acc.credentialRef]
    Write-Host ("  - {0} / {1} / {2}" -f $acc.provider, $acc.nickname, $uid)
}
Write-Host ""

# --- 收集待导入 -------------------------------------------------------------

$snapshotFiles = @(Get-ChildItem -LiteralPath $SnapshotDir -Filter '*.json' -File |
    Where-Object { $_.Name -ne 'index.json' } | Sort-Object Name)

$plan = [System.Collections.Generic.List[object]]::new()
$skipped = [System.Collections.Generic.List[object]]::new()

foreach ($file in $snapshotFiles) {
    $label = $file.Name
    try {
        $snap = [System.IO.File]::ReadAllText($file.FullName) | ConvertFrom-Json
    }
    catch {
        $skipped.Add([pscustomobject]@{ File = $label; Reason = "JSON 解析失败：$($_.Exception.Message)" })
        continue
    }

    $accessToken = [string]$snap.auth.accessToken
    $refreshToken = [string]$snap.auth.refreshToken
    $uid = [string]$snap.account.uid
    $nickname = [string]$snap.account.nickname
    $domain = [string]$snap.auth.domain

    # 池子的 isRefreshable() 直接取 refresh_token.length，缺了会抛，必须挡住
    if (-not $accessToken) {
        $skipped.Add([pscustomobject]@{ File = $label; Reason = '缺 auth.accessToken' }); continue
    }
    if (-not $refreshToken) {
        $skipped.Add([pscustomobject]@{ File = $label; Reason = '缺 auth.refreshToken（池子会抛异常）' }); continue
    }
    if (-not $uid) {
        $skipped.Add([pscustomobject]@{ File = $label; Reason = '缺 account.uid' }); continue
    }
    if (-not $nickname) { $nickname = $uid.Substring(0, 8) }

    $provider = $DomainToProvider[$domain]
    if (-not $provider) {
        $skipped.Add([pscustomobject]@{ File = $label; Reason = "domain '$domain' 不属于任何渠道" }); continue
    }

    if ($existingUids.ContainsKey($uid)) {
        $skipped.Add([pscustomobject]@{
            File   = $label
            Reason = "uid 已在池中（$($existingUids[$uid])），跳过以免重复"
        })
        continue
    }

    # ref 名规则（pack.js）：<PROVIDER>_ACCOUNT_<6+ 位十六进制>。用 uid 前 8 位，
    # 既满足正则又可读、不会撞。
    $suffix = ($uid -replace '-', '').Substring(0, 8).ToUpperInvariant()
    $refName = "{0}_ACCOUNT_{1}" -f $provider.ToUpperInvariant(), $suffix
    if ($existingRefs.ContainsKey($refName)) {
        $skipped.Add([pscustomobject]@{ File = $label; Reason = "ref 名 $refName 已存在" }); continue
    }

    # 凭据形状严格对齐 pack.js 的 buildCredential()，键顺序也照抄
    $credential = [ordered]@{
        access_token       = $accessToken
        refresh_token      = $refreshToken
        expires_at         = $snap.auth.expiresAt
        refresh_expires_at = $snap.auth.refreshExpiresAt
        token_type         = 'Bearer'
        scope              = ''
        domain             = $domain
        user_id            = $uid
        nickname           = $nickname
        enterprise_id      = ''
        account_type       = 'personal'
    }
    $json = $credential | ConvertTo-Json -Compress -Depth 5

    $plan.Add([pscustomobject]@{
        File       = $label
        Provider   = $provider
        Nickname   = $nickname
        Uid        = $uid
        Domain     = $domain
        RefName    = $refName
        AccountId  = ("{0}-{1}" -f $provider, $suffix.ToLowerInvariant())
        ExpiresAt  = $snap.auth.expiresAt
        Json       = $json
    })
}

Write-Host "待导入 $($plan.Count) 个："
foreach ($item in $plan) {
    Write-Host ("  + {0} / {1} / {2} / {3}" -f $item.Provider, $item.Nickname, $item.RefName, $item.Domain)
    # 只报形状与长度，绝不回显令牌本身：这段输出会留在终端回滚缓冲、会话记录里。
    $shape = $item.Json | ConvertFrom-Json
    Write-Host ("      ref 值 = access_token({0} 字符) refresh_token({1} 字符) expires_at={2} domain={3} user_id={4}" -f `
        $shape.access_token.Length, $shape.refresh_token.Length, $shape.expires_at, $shape.domain, $shape.user_id)
}
if ($skipped.Count -gt 0) {
    Write-Host ""
    Write-Host "跳过 $($skipped.Count) 个："
    foreach ($item in $skipped) {
        Write-Host ("  - {0}：{1}" -f $item.File, $item.Reason)
    }
}
Write-Host ""

if ($plan.Count -eq 0) {
    Write-Host "没有需要导入的账号，未改动任何文件。" -ForegroundColor Green
    return
}

if (-not $Apply) {
    Write-Host "== 以上为预演，未写入任何文件 ==" -ForegroundColor Cyan
    Write-Host "确认无误后：完全退出 DSH，再运行  pwsh -File $PSCommandPath -Apply" -ForegroundColor Cyan
    return
}

# --- 落盘 -------------------------------------------------------------------

$stamp = Get-Date -Format 'yyyyMMdd-HHmmss'
$backupDir = Join-Path $DshHome 'codebuddy-accounts-import-backup'
New-Item -ItemType Directory -Path $backupDir -Force | Out-Null

Copy-Item -LiteralPath $credsPath -Destination (Join-Path $backupDir ".credentials.yaml.$stamp") -Force
Copy-Item -LiteralPath $statePath -Destination (Join-Path $backupDir "state.json.$stamp") -Force
Write-Host "已备份到 $backupDir" -ForegroundColor Green

function Test-HasBom([string]$path) {
    $fs = [System.IO.File]::OpenRead($path)
    try {
        $b = New-Object byte[] 3
        $n = $fs.Read($b, 0, 3)
        return ($n -eq 3 -and $b[0] -eq 0xEF -and $b[1] -eq 0xBB -and $b[2] -eq 0xBF)
    }
    finally { $fs.Dispose() }
}

function Write-Atomic([string]$path, [string[]]$lines, [bool]$bom) {
    $dir = Split-Path -Parent $path
    $tmp = Join-Path $dir ("." + (Split-Path -Leaf $path) + "." + $PID + ".tmp")
    $enc = [System.Text.UTF8Encoding]::new($bom)
    [System.IO.File]::WriteAllText($tmp, (($lines -join "`n") + "`n"), $enc)
    Move-Item -LiteralPath $tmp -Destination $path -Force
}

# 1) .credentials.yaml：在 refs: 段末尾追加
$credsBom = Test-HasBom $credsPath
$newCredsLines = [System.Collections.Generic.List[string]]::new()
$newCredsLines.AddRange([string[]]$credsLines)

$insertAt = $refsEnd
foreach ($item in $plan) {
    # YAML 单引号标量：内部单引号需写两遍。JWT 不会带单引号，这里只是兜底。
    $escaped = $item.Json.Replace("'", "''")
    # 先把整行拼好再 Insert：写成 Insert($i, "..." -f $a, $b) 会让 -f 只绑定到
    # 第一个参数，{1} 落空并抛 "Index must be ... less than the size of the argument list"。
    $line = "  {0}: '{1}'" -f $item.RefName, $escaped
    $newCredsLines.Insert($insertAt, $line)
    $insertAt++
}
Write-Atomic $credsPath $newCredsLines.ToArray() $credsBom
Write-Host "已写入 $credsPath（+$($plan.Count) 条凭据）"

# 2) channel-pack/state.json：追加账号条目
$stateBom = Test-HasBom $statePath
$accounts = [System.Collections.Generic.List[object]]::new()
foreach ($acc in $stateAccounts) {
    $accounts.Add([ordered]@{
        id            = $acc.id
        provider      = $acc.provider
        nickname      = $acc.nickname
        enabled       = $acc.enabled
        credentialRef = $acc.credentialRef
        refreshable   = $acc.refreshable
        createdAt     = $acc.createdAt
        expiresAt     = $acc.expiresAt
    })
}
foreach ($item in $plan) {
    $accounts.Add([ordered]@{
        id            = $item.AccountId
        provider      = $item.Provider
        nickname      = $item.Nickname
        enabled       = $true
        credentialRef = $item.RefName
        refreshable   = $true
        createdAt     = [DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds()
        expiresAt     = $item.ExpiresAt
    })
}

$newState = [ordered]@{
    accounts             = @($accounts)
    disabledModels       = $state.disabledModels
    loomyPermanentLocked = $state.loomyPermanentLocked
    gatewayEnabled       = $state.gatewayEnabled
    providerOrder        = $state.providerOrder
}
$stateJson = $newState | ConvertTo-Json -Depth 20
Write-Atomic $statePath ($stateJson -split "`r?`n") $stateBom
Write-Host "已写入 $statePath（账号 $($stateAccounts.Count) -> $($accounts.Count)）"

# --- 收尾 -------------------------------------------------------------------

Write-Host ""
Write-Host "完成。现在重启 DSH，让它重新 load() 账号池。" -ForegroundColor Green
Write-Host "验证：白嫖模型接入页应能看到新账号；用量按账号分别记在 channel-pack/token-ledger.json。"
Write-Host ""
Write-Host "注意：加账号不会让模型选择器多出模型——账号池只用于同批模型的限额轮换，"
Write-Host "      数组顺序即优先级，某个号被限流会自动换下一个。"

<#
.SYNOPSIS
    为 PetPC 的语音朗读功能安装一个本地 GPT-SoVITS 合成服务（P0 阶段）。

.DESCRIPTION
    这一步与 PetPC 本身完全解耦：脚本只负责把合成服务装起来并跑通，不改动
    PetPC 一行代码。装好之后，PetPC 只是一个 HTTP 客户端。

    它做四件事：
      1. 取源码（CPUFast 分支，见下）
      2. 建独立 uv 虚拟环境（Python 3.11），装 CPU 版 PyTorch 与依赖
      3. 下载推理所需底模（HuBERT / RoBERTa / G2PW / 版本权重）
      4. 可选：下载一个原神角色模型（默认珊瑚宫心海）并生成配置

.PARAMETER Root
    安装位置。默认 D:\GPT-SoVITS —— 不要装在 C 盘（20GB 剩余）或 F 盘（项目盘，
    13GB 剩余），整体需要约 8-10GB。

.PARAMETER Character
    要下载的角色模型。默认 kokomi（珊瑚宫心海，HF 上可直接下载且中文可用）。
    换角色只是换权重与参考音频，PetPC 侧只需改配置里的三个路径。

.PARAMETER SkipCharacter
    只装基础服务，不下载角色模型。适合想自己准备模型的情况。

.PARAMETER Source
    底模下载源：HF-Mirror（默认）/ HF / ModelScope。

.NOTES
    为什么用 CPUFast 分支而不是官方 main：
      官方 install.ps1 需要 conda（这台机器没有），且 requirements 里
      `--no-binary=opencc` 要求本地编译 opencc（需要 cmake，也没有）。
      CPUFast 是官方文档点名的 CPU 优化推理分支，用 uv 管理依赖、逐文件从
      hf-mirror 下载底模，既不需要 conda 也不需要 cmake。
      它的 api_v2.py 路由与参数和官方逐字一致（/tts、/set_gpt_weights、
      /set_sovits_weights，默认端口 9880），所以 PetPC 的客户端不用改。

    前置条件（脚本会先检查，缺什么会明确告诉你）：
      - uv（已安装：C:\Users\fan\.local\bin\uv.exe）
      - C/C++ 编译器：pyopenjtalk 在 PyPI 上只有源码包（无 Windows wheel），
        必须编译。这台机器已有 Visual Studio 2019 Community 的 C++ 工具集
        （D:\Visual Studio\Community\VC\Tools\MSVC\14.29.30133 与 Windows SDK），
        脚本会自动通过 vcvars64.bat 激活它。
#>

[CmdletBinding()]
param(
    [string]$Root = 'D:\GPT-SoVITS',
    [string]$Character = 'kokomi',
    [switch]$SkipCharacter,
    [ValidateSet('HF-Mirror', 'HF', 'ModelScope')]
    [string]$Source = 'HF-Mirror',
    # 只检查环境不实际安装，用于先看看这台机器差什么。
    [switch]$CheckOnly
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

# --- 输出helpers ------------------------------------------------------------

function Write-Step($msg) {
    Write-Host ""
    Write-Host "==> $msg" -ForegroundColor Cyan
}
function Write-Ok($msg) { Write-Host "    [OK]   $msg" -ForegroundColor Green }
function Write-Warn2($msg) { Write-Host "    [WARN] $msg" -ForegroundColor Yellow }
function Write-Err($msg) { Write-Host "    [FAIL] $msg" -ForegroundColor Red }
function Write-Note($msg) { Write-Host "           $msg" -ForegroundColor DarkGray }

function Fail($msg, $hint) {
    Write-Host ""
    Write-Err $msg
    if ($hint) {
        Write-Host ""
        Write-Host "    怎么办：" -ForegroundColor Yellow
        $hint -split "`n" | ForEach-Object { Write-Host "      $_" -ForegroundColor Yellow }
    }
    exit 1
}

# --- 常量 -------------------------------------------------------------------

$RepoUrl      = 'https://github.com/baicai-1145/GPT-SoVITS-CPUFast'
# gitee 上只有官方镜像；CPUFast 走 ghproxy 代理取源码包。
$SourceZipUrl = 'https://ghproxy.net/https://github.com/baicai-1145/GPT-SoVITS-CPUFast/archive/refs/heads/main.zip'
$PypiMirror   = 'https://pypi.tuna.tsinghua.edu.cn/simple'

# 角色模型清单。每个条目给出 GPT 权重、SoVITS 权重与参考音频在 HF 仓库中的路径。
#
# 参考音频必须与文本同语言，否则跨语言会明显掉质量，所以中文输出优先选中文 wav。
$Characters = @{
    'kokomi' = @{
        Label    = '珊瑚宫心海（中/日/英）'
        Repo     = 'xiaoheiqaq/GPT-Sovits-models'
        # 该仓库提供的 GPT 权重:  kokomi2-e4/e6/e8/e10.ckpt
        #              SoVITS 权重:  kokomi2_e1_s153 / e4_s612 / e8_s1224 /
        #                            e11_s1683 / e15_s2295 .pth
        # 两边 epoch 都存在的只有 e4 与 e8。这里取 e8（训练更充分，又不到
        # e10/e15 那样可能过拟合）。想换任何一个只需改下面两行。
        GptPath  = 'kokomi/kokomi2-e8.ckpt'
        Sovits   = 'kokomi/kokomi2_e8_s1224.pth'
        # readme 里演示用的中文样例音频，其文本在 README 中给出。
        # 用模型自己的输出当参考是 GPT-SoVITS 支持的用法（音色一致即可）。
        RefAudio = 'readme.assets/kokomi/chinese1.wav'
        Prompt   = '今天的天气非常好，阳光明媚，非常适合出去郊游。'
        Lang     = 'zh'
    }
}

# --- 环境检查 ---------------------------------------------------------------

Write-Host ""
Write-Host "PetPC 语音朗读 · P0 安装（本地 GPT-SoVITS 合成服务）" -ForegroundColor White
Write-Host "==========================================================" -ForegroundColor DarkGray
Write-Note "安装位置: $Root"
Write-Note "角色模型: $(if ($SkipCharacter) { '跳过' } else { $Character })"
Write-Note "底模来源: $Source"

Write-Step '检查前置条件'

# 1. uv
$uv = (Get-Command uv -ErrorAction SilentlyContinue).Source
if (-not $uv) {
    Fail '没有找到 uv。' @"
uv 是 Python 包管理器，脚本用它建独立环境（不污染系统 Python 3.13）。
安装（PowerShell）：
  powershell -c "irm https://astral.sh/uv/install.ps1 | iex"
装完重开一个终端再跑本脚本。
"@
}
Write-Ok "uv: $uv"

# 2. C/C++ 编译器 —— pyopenjtalk 只有源码包，必须编译
$vcvars = $null
$vswhere = Join-Path ${env:ProgramFiles(x86)} 'Microsoft Visual Studio\Installer\vswhere.exe'
if (Test-Path $vswhere) {
    $vsPath = (& $vswhere -latest -products '*' -requires Microsoft.VisualStudio.Component.VC.Tools.x86.x64 -property installationPath 2>$null | Select-Object -First 1)
    if ($vsPath) {
        $candidate = Join-Path $vsPath.Trim() 'VC\Auxiliary\Build\vcvars64.bat'
        if (Test-Path $candidate) { $vcvars = $candidate }
    }
}
$hasCompiler = (Get-Command cl -ErrorAction SilentlyContinue) -or
               (Get-Command gcc -ErrorAction SilentlyContinue) -or
               (Get-Command clang -ErrorAction SilentlyContinue)

if ($hasCompiler) {
    Write-Ok 'C/C++ 编译器已在 PATH 中'
} elseif ($vcvars) {
    Write-Ok "Visual Studio C++ 工具集: $vcvars"
    Write-Note 'pyopenjtalk 需要编译，脚本会在装依赖时自动激活它'
} else {
    Fail '没有找到 C/C++ 编译器。' @"
pyopenjtalk 在 PyPI 上只有源码包（没有 Windows 预编译 wheel），必须本地编译。

最省事的办法是装 Visual Studio Build Tools 的「使用 C++ 的桌面开发」工作负载：
  https://visualstudio.microsoft.com/visual-cpp-build-tools/
只需要勾选「MSVC v143 生成工具」和「Windows SDK」，不必装整个 IDE。
装完重开终端再跑本脚本。
"@
}

# 3. 磁盘空间
#
# Get-PSDrive's Free property comes back $null in some environments (it did on the
# machine this was written on), which would report "0GB free" and scare a user off a
# perfectly good disk. The Win32 call is the reliable source.
function Get-FreeGB([string]$path) {
    if (-not ('PetPC.Volume' -as [type])) {
        Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
namespace PetPC {
    public class Volume {
        [DllImport("kernel32.dll", SetLastError = true, CharSet = CharSet.Auto)]
        public static extern bool GetDiskFreeSpaceEx(string p, out ulong freeForCaller,
                                                     out ulong total, out ulong totalFree);
    }
}
'@
    }
    $qualifier = Split-Path -Qualifier $path
    # Parameter order matters and is easy to get wrong: the FIRST out parameter is
    # the space available to this caller, the second is the volume's total size, and
    # the third is total free. Reading the second reports the disk's full capacity
    # as if it were free space - which is the bug this comment exists to prevent.
    $freeToCaller = 0; $totalBytes = 0; $totalFree = 0
    if ([PetPC.Volume]::GetDiskFreeSpaceEx($qualifier, [ref]$freeToCaller, [ref]$totalBytes, [ref]$totalFree)) {
        return [math]::Round($freeToCaller / 1GB, 1)
    }
    return $null
}

$qualifier = Split-Path -Qualifier $Root
$freeGB = Get-FreeGB $Root
if ($null -eq $freeGB) {
    Write-Warn2 "读不到 $qualifier 的剩余空间，自己确认一下要留足 12GB"
} elseif ($freeGB -lt 12) {
    Write-Warn2 "$qualifier 只剩 ${freeGB}GB，建议至少 12GB（底模+依赖+角色约 8-10GB）"
} else {
    Write-Ok "$qualifier 剩余 ${freeGB}GB"
}

if ($CheckOnly) {
    Write-Host ""
    Write-Ok '环境检查通过（-CheckOnly 模式，未做任何改动）'
    exit 0
}

# --- 1. 取源码 --------------------------------------------------------------

Write-Step "取源码到 $Root"

if (Test-Path (Join-Path $Root 'pyproject.toml')) {
    Write-Ok '源码已存在，跳过下载'
} else {
    New-Item -ItemType Directory -Force -Path $Root | Out-Null
    $zip = Join-Path $env:TEMP 'gptsovits-cpufast.zip'
    Write-Note "下载 $SourceZipUrl"
    try {
        # curl.exe 比 Invoke-WebRequest 快很多，而且大文件下更稳。
        & curl.exe -sL --fail --max-time 600 -o $zip $SourceZipUrl
        if ($LASTEXITCODE -ne 0) { throw "curl 退出码 $LASTEXITCODE" }
    } catch {
        Fail "源码下载失败：$_" @"
请检查网络能否访问 ghproxy.net。也可以手动下载后解压：
  $SourceZipUrl
解压后的目录内容应直接位于 $Root（即 $Root\pyproject.toml 存在）。
"@
    }

    Write-Note '解压...'
    $tmp = Join-Path $env:TEMP 'gptsovits-cpufast-extract'
    Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
    Expand-Archive -Path $zip -DestinationPath $tmp -Force
    # 压缩包内有一层 GPT-SoVITS-CPUFast-main 目录，把它里面的内容提到 $Root。
    $inner = Get-ChildItem $tmp -Directory | Select-Object -First 1
    if (-not $inner) { Fail '解压后没有找到目录' }
    Get-ChildItem $inner.FullName -Force | ForEach-Object {
        Move-Item $_.FullName -Destination $Root -Force
    }
    Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
    Remove-Item $zip -Force -ErrorAction SilentlyContinue
    Write-Ok '源码就绪'
}

# --- 2. Python 环境 ---------------------------------------------------------

Write-Step '建独立 Python 环境并安装依赖'

Push-Location $Root
try {
    # uv 默认把缓存放在 %LOCALAPPDATA%\uv，C 盘紧张时很容易撑爆。项目内缓存，
    # 这样卸载时删目录就干净了。
    $env:UV_CACHE_DIR = Join-Path $Root '.uv-cache'
    # 国内直连 PyPI 很慢，走清华源。
    $env:UV_DEFAULT_INDEX = $PypiMirror
    $env:UV_INDEX_URL = $PypiMirror

    if (Test-Path (Join-Path $Root '.venv')) {
        Write-Ok '虚拟环境已存在，跳过创建'
    } else {
        Write-Note 'uv sync（第一次会下载 CPU 版 PyTorch，约 2-3GB，需要几分钟）'
        # pyproject 要求 >=3.10,<3.13，用 3.11（uv 已备好该版本）。
        & uv sync --python 3.11 --locked
        if ($LASTEXITCODE -ne 0) {
            Write-Warn2 'uv sync --locked 失败，回退到不带 --locked 重试'
            & uv sync --python 3.11
            if ($LASTEXITCODE -ne 0) { Fail "uv sync 失败（退出码 $LASTEXITCODE）" }
        }
        Write-Ok '依赖装好了'
    }

    $venvPython = Join-Path $Root '.venv\Scripts\python.exe'
    if (-not (Test-Path $venvPython)) { Fail "没有找到 $venvPython" }
    Write-Ok "venv python: $venvPython"
} finally {
    Pop-Location
}

# --- 3. 底模 ----------------------------------------------------------------

Write-Step '下载推理底模'

# 官方 CPUFast install.ps1 里的下载逻辑很完整（逐文件、幂等、多源），
# 与其在这里重写一遍，不如直接调用它 —— 这样上游更新底模清单时我们自动跟上。
$upstreamInstall = Join-Path $Root 'install.ps1'
if (-not (Test-Path $upstreamInstall)) {
    Fail "没有找到上游 install.ps1（$upstreamInstall）" @"
源码包可能不完整。请确认 $Root 下存在 install.ps1。
"@
}

Push-Location $Root
try {
    # 这条脚本自己会 Set-Location $PSScriptRoot，并强制要求 uv 与 C++ 编译器。
    # 它把底模装到 $Root（默认 GPT_SOVITS_DATA_DIR = $PSScriptRoot）。
    Write-Note "调用上游 install.ps1 -Source $Source -Version v2"
    Write-Note '（v2 是稳定版；v4 更重，CPU 上更慢，P0 先用 v2 测速度）'
    & powershell -NoProfile -ExecutionPolicy Bypass -File $upstreamInstall -Source $Source -Version v2
    if ($LASTEXITCODE -ne 0) {
        Fail "上游 install.ps1 失败（退出码 $LASTEXITCODE）" @"
常见原因：
  1. pyopenjtalk 编译失败 —— 确认 Visual Studio 的 C++ 工作负载已装。
  2. hf-mirror 下载中断 —— 重新跑一次本脚本，已下载的部分会跳过。
"@
    }
    Write-Ok '底模就绪'
} finally {
    Pop-Location
}

# --- 4. 角色模型 ------------------------------------------------------------

if ($SkipCharacter) {
    Write-Step '跳过角色模型（-SkipCharacter）'
    Write-Note '之后可以再跑一次不带该开关的脚本，或手动把权重放进 models\ 目录'
} else {
    Write-Step "下载角色模型：$Character"

    if (-not $Characters.ContainsKey($Character)) {
        $known = ($Characters.Keys | Sort-Object) -join ', '
        Fail "未知角色 '$Character'（已知：$known）" @"
要加新角色，编辑本脚本里的 `$Characters` 表：
  Repo / GptPath / Sovits / RefAudio / Prompt / Lang
HF 上可用的原神 GPT-SoVITS 模型见 docs\genshin-tts.md §3。
"@
    }
    $ch = $Characters[$Character]
    $modelDir = Join-Path $Root "models\$Character"
    New-Item -ItemType Directory -Force -Path $modelDir | Out-Null

    # HF-Mirror 对国内最稳；三个源的文件路径结构一致。
    $repoBase = switch ($Source) {
        'HF-Mirror' { "https://hf-mirror.com/$($ch.Repo)/resolve/main" }
        'HF' { "https://huggingface.co/$($ch.Repo)/resolve/main" }
        'ModelScope' { "https://www.modelscope.cn/models/$($ch.Repo)/resolve/master" }
    }

    function Get-ModelFile($relPath, $destName) {
        $dest = Join-Path $modelDir $destName
        if (Test-Path $dest) {
            Write-Ok "$destName 已存在"
            return $dest
        }
        $url = "$repoBase/$relPath"
        Write-Note "下载 $destName"
        & curl.exe -sL --fail --max-time 900 -o $dest $url
        if ($LASTEXITCODE -ne 0) {
            Fail "下载失败：$url" @"
请检查网络。也可以手动下载后放到：
  $dest
"@
        }
        $sizeMB = [math]::Round((Get-Item $dest).Length / 1MB, 1)
        Write-Ok "$destName ($sizeMB MB)"
        return $dest
    }

    $gptFile = Get-ModelFile $ch.GptPath   'gpt.ckpt'
    $sovFile = Get-ModelFile $ch.Sovits    'sovits.pth'
    $refFile = Get-ModelFile $ch.RefAudio  'ref.wav'

    # 生成 PetPC 能直接用的配置片段。写成一个独立文件而不是让用户手抄，
    # 因为这些路径很长、含中文与反斜杠，抄错一个字符就会 404。
    $cfgFile = Join-Path $modelDir 'petpc-config.json'
    $snippet = [ordered]@{
        ttsEnabled       = $true
        ttsBaseUrl       = 'http://127.0.0.1:9880'
        ttsCharacter     = $ch.Label
        ttsGptWeights    = $gptFile
        ttsSovitsWeights = $sovFile
        ttsRefAudioPath  = $refFile
        ttsPromptText    = $ch.Prompt
        ttsLang          = $ch.Lang
    }
    $snippet | ConvertTo-Json -Depth 4 | Set-Content -Path $cfgFile -Encoding UTF8
    Write-Ok "PetPC 配置片段: $cfgFile"
}

# --- 完成 -------------------------------------------------------------------

Write-Host ""
Write-Host "==========================================================" -ForegroundColor DarkGray
Write-Host " 安装完成" -ForegroundColor Green
Write-Host "==========================================================" -ForegroundColor DarkGray
Write-Host ""
Write-Host " 下一步：" -ForegroundColor White
Write-Host ""
Write-Host "  1) 启动合成服务（另开一个终端，保持运行）：" -ForegroundColor White
Write-Host "       cd $Root" -ForegroundColor Gray
if (-not $SkipCharacter) {
    Write-Host "       .venv\Scripts\python.exe api_v2.py -a 127.0.0.1 -p 9880 -c GPT_SoVITS\configs\tts_infer.yaml" -ForegroundColor Gray
} else {
    Write-Host "       .venv\Scripts\python.exe api_v2.py -a 127.0.0.1 -p 9880 -c GPT_SoVITS\configs\tts_infer.yaml" -ForegroundColor Gray
}
Write-Host ""
Write-Host "  2) 测速度（这是 P0 的真正目的）：" -ForegroundColor White
Write-Host "       pwsh -File tools\tts-p0\benchmark.ps1 -Root $Root$(if (-not $SkipCharacter) { " -Character $Character" })" -ForegroundColor Gray
Write-Host ""
Write-Host "  3) 把角色配置并进 PetPC：" -ForegroundColor White
if (-not $SkipCharacter) {
    Write-Host "       把 $modelDir\petpc-config.json 里的字段并进" -ForegroundColor Gray
} else {
    Write-Host "       （本次跳过了角色模型，先自己准备好权重）" -ForegroundColor Gray
}
Write-Host "       %AppData%\PetPC\config.json ，然后重启 PetPC。" -ForegroundColor Gray
Write-Host "       也可以等设置面板 UI 做好后在界面里填。" -ForegroundColor Gray
Write-Host ""
Write-Host "  参考：docs\genshin-tts.md §9 有判定标准（RTF ≤1.5 可接受，>2.5 建议放弃实时合成）。" -ForegroundColor DarkGray
Write-Host ""

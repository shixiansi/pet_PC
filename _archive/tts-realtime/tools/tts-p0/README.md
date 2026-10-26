# P0：本地 GPT-SoVITS 语音服务（安装 + 基准测试）

这两个脚本对应 `docs/genshin-tts.md` 的 **P0 阶段**：把合成服务装起来并测出三个数，
**不改动 PetPC 一行代码**。装好之后 PetPC 只是一个 HTTP 客户端。

## 为什么需要它

`docs/genshin-tts.md` §9 定了判定标准，但前提是先有真实数字。这三个数决定整套
方案能不能用：

| 测出来的 RTF | 结论 |
|---|---|
| **≤ 1.5** | 可以按原方案做（边生成边朗读） |
| **1.5 ~ 2.5** | 缩范围：只朗读主动说话与番茄钟，不读长回复 |
| **> 2.5** | 放弃实时合成，改走「台词库」方案 |

参考基线（官方 README）：M4 CPU RTF 0.526。你这台 i5-11300H 虽弱于 M4，但实测
**AVX-512 + VNNI 都可用**（Tiger Lake 多数型号会熔断，这颗没有），VNNI 正是量化
推理最吃的指令，所以结果值得实测。

## 用哪个源码分支，为什么

用的是 **CPUFast 分支**（`baicai-1145/GPT-SoVITS-CPUFast`），不是官方 `main`：

| | 官方 main | CPUFast |
|---|---|---|
| 依赖 conda | **是**（装 ffmpeg/cmake） | 否（用 uv） |
| `--no-binary=opencc` 需编译 | **是**（需 cmake） | 否 |
| 面向 CPU 优化 | 否 | **是** |

官方 `main` 的 `install.ps1` 第一件事就是 `conda install ffmpeg cmake`，而这台机器
没有 conda。CPUFast 是官方文档点名的 CPU 优化推理分支，`api_v2.py` 的路由与参数与
官方**逐字一致**（`/tts`、`/set_gpt_weights`、`/set_sovits_weights`，默认端口 9880），
所以 PetPC 已实现的客户端不用改。

## 下载不稳时：手动抓包

`ghproxy.net` 下大文件会中途断（实测源码包就断在 4.1MB）。这时用 `fetch.ps1`
打印清单，拿 URL 去用下载器工具（IDM / aria2 / 迅雷）：

```powershell
# 只打印清单和 URL，不下载
powershell -ExecutionPolicy Bypass -File tools\tts-p0\fetch.ps1

# 或者让脚本自己下（带断点续传，断了重跑会接着下）
powershell -ExecutionPolicy Bypass -File tools\tts-p0\fetch.ps1 -Download -Group base
```

**两个关键点**（都是实测踩出来的）：

1. **不要下那个 4.35GB 的 `pretrained_models.zip`**。
   官方 CPUFast 脚本其实是**逐文件**下载的，v2 的最小集合约 **2GB**
   （base 1.04GB + g2pw 0.61GB + char 0.23GB）。那个 zip 里还含
   v1/v3/v4/v2Pro 的权重，本方案用不到。

2. **URL 路径不带 `GPT_SoVITS/` 前缀，但本地落盘必须带**。
   - 下载用：`.../resolve/main/pretrained_models/chinese-hubert-base/...`
   - 落盘到：`$Root\GPT_SoVITS\pretrained_models\chinese-hubert-base\...`

   因为 `tts_infer.yaml` 里写的是相对路径 `GPT_SoVITS/pretrained_models/...`。
   带前缀去下载会 404，不带前缀落盘则服务找不到模型。

**源码包换镜像**：`gh-proxy.com` 实测能下完，`ghproxy.net` 会断。

```powershell
# 手动下源码包（7.2MB）
curl -L -o "$env:TEMP\cpufast.zip" "https://gh-proxy.com/https://github.com/baicai-1145/GPT-SoVITS-CPUFast/archive/refs/heads/main.zip"
```

下完源码 zip 后解压，把里面那层目录（`GPT-SoVITS-CPUFast-main`）的**内容**放进
`$Root`，即 `$Root\pyproject.toml` 存在。之后重跑 `install.ps1`——它会检测到文件
已存在而跳过下载，只做建环境与装依赖。

## 前置条件（`install.ps1 -CheckOnly` 会检查）

| 条件 | 你这台机器 |
|---|---|
| **uv** | ✅ `C:\Users\fan\.local\bin\uv.exe`（0.11.6） |
| **C/C++ 编译器** | ✅ VS2019 Community `D:\Visual Studio\Community`（含 MSVC 14.29 + Windows SDK） |
| 磁盘 | ✅ D 盘 95.6GB（脚本默认装 `D:\GPT-SoVITS`） |
| Python | 无需干预：uv 自带 3.11（**系统 Python 3.13 太新，GPT-SoVITS 要求 3.10~3.12**） |

**为什么必须要 C++ 编译器**：`pyopenjtalk` 在 PyPI 上**只有源码包，没有 Windows
wheel**，必须本地编译。已实测用你的 VS2019 工具链能成功编出
`pyopenjtalk-0.4.1-cp311-cp311-win_amd64.whl`。

## 用法

### 1. 先看看环境差什么（不改动任何东西）

```powershell
powershell -ExecutionPolicy Bypass -File tools\tts-p0\install.ps1 -CheckOnly
```

### 2. 安装

```powershell
powershell -ExecutionPolicy Bypass -File tools\tts-p0\install.ps1
```

默认装到 `D:\GPT-SoVITS`，并下载**珊瑚宫心海**模型（HF 上可直接下载、中文可用）。
预计下载 8~10GB。

想换位置或只装服务：

```powershell
# 换盘
powershell -ExecutionPolicy Bypass -File tools\tts-p0\install.ps1 -Root E:\GPT-SoVITS

# 只装服务，角色模型自己准备
powershell -ExecutionPolicy Bypass -File tools\tts-p0\install.ps1 -SkipCharacter
```

安装是**幂等**的：中断后重跑，已下载的部分会跳过。

### 3. 测速度（P0 的真正目的）

```powershell
# 让脚本自己起服务并测冷启动（推荐）
powershell -ExecutionPolicy Bypass -File tools\tts-p0\benchmark.ps1 -Root D:\GPT-SoVITS -StartServer
```

或者你自己在一个终端跑着服务，只测速度：

```powershell
# 终端 A：起服务
cd D:\GPT-SoVITS
.venv\Scripts\python.exe api_v2.py -a 127.0.0.1 -p 9880 -c GPT_SoVITS\configs\tts_infer.yaml

# 终端 B：测速
powershell -ExecutionPolicy Bypass -File tools\tts-p0\benchmark.ps1 -Root D:\GPT-SoVITS
```

### 4. 把结果接到 PetPC

安装脚本会生成 `D:\GPT-SoVITS\models\kokomi\petpc-config.json`。把里面的字段并进
`%AppData%\PetPC\config.json`（**并进现有对象，不要整个覆盖**），然后重启 PetPC：

```json
{
  "ttsEnabled": true,
  "ttsBaseUrl": "http://127.0.0.1:9880",
  "ttsCharacter": "珊瑚宫心海（中/日/英）",
  "ttsGptWeights": "D:\\GPT-SoVITS\\models\\kokomi\\gpt.ckpt",
  "ttsSovitsWeights": "D:\\GPT-SoVITS\\models\\kokomi\\sovits.pth",
  "ttsRefAudioPath": "D:\\GPT-SoVITS\\models\\kokomi\\ref.wav",
  "ttsPromptText": "今天的天气非常好，阳光明媚，非常适合出去郊游。",
  "ttsLang": "zh"
}
```

> 设置面板 UI 还没做（见 `docs/genshin-tts.md` §16.5），所以现在只能手改 JSON。

## 换角色

改 `install.ps1` 里 `$Characters` 表的一项，或直接手动把权重放好再改 config：

```powershell
powershell -ExecutionPolicy Bypass -File tools\tts-p0\install.ps1 -Character <名字>
```

**换角色不需要改 PetPC 代码**——只是换三个文件路径。可选模型见
`docs/genshin-tts.md` §3。

## ⚠️ 关于 PowerShell 版本与中文乱码

这台机器的 `pwsh` 实际是 **Windows PowerShell 5.1**（`PSEdition=Desktop`）。它会把
不带 BOM 的 `.ps1` 按**系统 ANSI 代码页（GBK）**解析，于是中文注释会让解析器报出
莫名其妙的错，比如在看起来完全正常的一行上说"字符串缺少终止符"。

所以这三个脚本都**必须带 UTF-8 BOM**。如果你用编辑器改动它们后脚本突然报语法错，
先跑：

```powershell
powershell -ExecutionPolicy Bypass -File tools\tts-p0\fix-bom.ps1 -Path tools\tts-p0\*.ps1
```

`fix-bom.ps1` 是**纯字节操作**（只在开头插入三个字节），不经过文本解码，所以不会
把中文改坏。

## 排查

| 现象 | 原因与处理 |
|---|---|
| `没有找到 C/C++ 编译器` | 装 VS Build Tools 的「使用 C++ 的桌面开发」工作负载，或确认 `vswhere` 能找到 VS |
| `uv sync` 失败 | 确认能访问清华 PyPI 源；脚本已把 `UV_CACHE_DIR` 指到项目内，避免 C 盘被缓存撑爆 |
| 底模下载中断 | 直接重跑，已下载部分会跳过；也可换 `-Source ModelScope` |
| 冷启动超过 1 分钟 | 正常（首次要加载底模）；`benchmark.ps1` 给 5 分钟超时 |
| RTF 明显偏高 | 确认没有别的程序在抢 CPU；CPU 推理对后台负载很敏感 |
| 端口 9880 被占用 | `benchmark.ps1 -Port <别的端口>`；服务端也要用同一个端口 |

## 清理

整个安装是自包含的，删目录即可完全卸载：

```powershell
Remove-Item D:\GPT-SoVITS -Recurse -Force
```

PetPC 侧把 `ttsEnabled` 改回 `false` 就恢复成"完全不出声"的默认行为。

# Workspace-local Go/Node toolchain environment.
#
# The DSH file sandbox allows writes only under F:\pet_pc, so the Go toolchain,
# module cache and the Wails CLI all live here instead of the user profile.
# Dot-source this before any go/wails/npm command.

$WorkspaceRoot = Split-Path -Parent $PSScriptRoot

# Go 1.22.12 is installed locally because Wails v2.10+ requires Go >= 1.22 and
# the machine's own Go is 1.21 - and GOTOOLCHAIN=local (below) forbids switching
# to a newer one. Without this directory NO gate can run: every go command exits
# with "go.mod requires go >= 1.22.0 (running go 1.21.0; GOTOOLCHAIN=local)", and
# that is a toolchain report rather than a compile error, so it reads like the
# repository is broken when only this path is.
#
# Two locations, because this workspace has had the toolchain in both: the
# primary .dev\gotool\go, and the nested sandbox copy under pet_pc\.dev\gotool\go
# that an earlier provisioning pass left behind. The primary wins when it exists,
# so this file means the same thing on a machine that has either one.
$LocalGoCandidates = @(
    (Join-Path $WorkspaceRoot '.dev\gotool\go'),
    (Join-Path $WorkspaceRoot 'pet_pc\.dev\gotool\go')
)
$LocalGo = $LocalGoCandidates | Where-Object { Test-Path (Join-Path $_ 'bin\go.exe') } | Select-Object -First 1
if ($LocalGo) {
    $env:GOROOT = $LocalGo
    $env:PATH   = "$(Join-Path $LocalGo 'bin');$env:PATH"
}

$env:GOPATH       = Join-Path $WorkspaceRoot '.dev\gopath'
$env:GOMODCACHE   = Join-Path $env:GOPATH 'pkg\mod'
$env:GOCACHE      = Join-Path $WorkspaceRoot '.dev\gocache'
$env:GOBIN        = Join-Path $WorkspaceRoot '.dev\bin'
$env:GOTMPDIR     = Join-Path $WorkspaceRoot '.dev\tmp'

# proxy.golang.org is unreachable from this machine; use the CN mirror.
$env:GOPROXY      = 'https://goproxy.cn,direct'
$env:GOSUMDB      = 'off'
$env:GOFLAGS      = ''
$env:GOTOOLCHAIN  = 'local'   # never try to fetch another toolchain

# npm already defaults to registry.npmmirror.com in the user config,
# but its cache/log dirs default to %LOCALAPPDATA% which the sandbox denies.
$env:npm_config_registry = 'https://registry.npmmirror.com'
$env:npm_config_cache    = Join-Path $WorkspaceRoot '.dev\npm-cache'
$env:npm_config_logs_dir = Join-Path $WorkspaceRoot '.dev\npm-logs'
$env:npm_config_update_notifier = 'false'

foreach ($d in @($env:GOPATH, $env:GOMODCACHE, $env:GOCACHE, $env:GOBIN, $env:GOTMPDIR, $env:npm_config_cache, $env:npm_config_logs_dir)) {
    if (-not (Test-Path -LiteralPath $d)) { New-Item -ItemType Directory -Force -Path $d | Out-Null }
}

# Put the Wails CLI and this process on PATH.
$env:PATH = "$env:GOBIN;$(Join-Path $WorkspaceRoot '.dev\tools');$env:PATH"

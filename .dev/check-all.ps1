# Runs every gate this project has, in one command.
#
# Usage:  pwsh -File .dev\check-all.ps1        (or  powershell -File ...)
#
# BOTH shells work, and that depends on something invisible: every .ps1 in this repo is
# UTF-8 **with a BOM**. Windows PowerShell 5.1 decodes a BOM-less file as the system ANSI
# code page, and with GBK that mangles the Chinese text enough to eat a quote or a
# here-string terminator - the file then fails to PARSE with errors that point at innocent
# lines ("A 'using' statement must appear before any other statements"). This file had
# exactly that: 43 parse errors under powershell.exe, 0 under pwsh, entirely because of the
# missing BOM. So: never strip the BOM from these files, and the check below will notice if
# someone does.

# The gates, and why each exists:
#   1. go vet + go test     - the logic, including the SSE decoder, the plugin
#                             sandbox and the chat state machine.
#   2. market go vet + test - the plugin market is a SEPARATE program with its own
#                             module, so the pet's `go vet ./...` never sees it.
#                             Without this gate the market's Go would be the only
#                             code in the repo with no test run at all.
#   3. frontend builds      - ALL the bundles must compile; catches a JS syntax error
#                             or an import of a binding the program does not export.
#                             Three builds, because the pet's frontend, the market's old
#                             frontend and the control center are three different apps
#                             against two different binding sets. The last step copies the
#                             control center's output over `frontend/dist`, which is the
#                             tree the market's Go binary embeds - doing it anywhere else
#                             would let the gate below ship the PREVIOUS interface.
#   4. control center checks - svelte-check over the control center (so a payload field the
#                             host does not send is a compile error rather than a blank
#                             row) and a jsdom render of all six pages against the dev
#                             stub (so "it rendered" is never confused with "it rendered
#                             the host's numbers").
#   5. static wiring checks - every element id, every binding call and every
#                             subscribed event has a counterpart, and the new CSS
#                             breaks none of the window-region rules.
#   6. plugin front-end tests - the PLUGINS' OWN app.js, in jsdom, against a fake
#                             host. This is the only gate that executes plugin
#                             code: the static checks only read it as text, and the
#                             harness runs the pet's page against a fake backend.
#   7. behaviour harness    - the real bundle in a real browser engine against a
#                             fake backend: click/double-click, the panel slot, the
#                             plugin surface's geometry and protocol, settings
#                             round trips.
#   8. wails build          - the shipping artifact must link.
#   9. region pixel checks  - the SHIPPING binary against the real desktop: the
#                             panel/bubble/overlay crop, the pet not moving, and
#                             the thought puffs actually being ellipses. This is
#                             the only gate that can see the native window region,
#                             so it runs after the build produced the artifact.

$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'env.ps1')

$root = Split-Path -Parent $PSScriptRoot
$results = @()
# Pixel gates that were skipped because this session has no desktop able to host a
# WebView2 window. They are reported separately from PASS so a skipped gate is never
# mistaken for a gate that checked something.
$skipped = @()
function Gate($name, [scriptblock] $body) {
    Write-Host ""
    Write-Host "=== $name ===" -ForegroundColor Cyan
    $ok = $false
    # npm and the wails CLI both write ordinary progress and deprecation notices
    # to stderr. Under $ErrorActionPreference = 'Stop' a native command's stderr
    # is promoted to a terminating error, which would report a successful build
    # as a failure - so the preference is relaxed for the duration of the gate
    # and the verdict comes from $LASTEXITCODE alone.
    $previous = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try {
        & $body
        $ok = ($LASTEXITCODE -eq 0)
    } catch {
        Write-Host $_ -ForegroundColor Red
        $ok = $false
    } finally {
        $ErrorActionPreference = $previous
    }
    $script:results += [pscustomobject]@{ Gate = $name; Ok = $ok }
}

Push-Location $root
try {
    # Every .ps1 must be UTF-8 WITH a BOM - see the header for what a stripped BOM does.
    # It is a gate rather than a comment because a removed BOM is invisible in a diff and
    # shows up only as a wall of parse errors for whoever runs the script under Windows
    # PowerShell, blaming lines that are perfectly fine.
    Gate 'script encodings' {
        $missingBom = @()
        Get-ChildItem -Path (Join-Path $root '.dev'), (Join-Path $root 'tools') -Filter *.ps1 -Recurse |
            Where-Object { $_.FullName -notmatch '\\node_modules\\' } |
            ForEach-Object {
                $head = New-Object byte[] 3
                $fs = [System.IO.File]::OpenRead($_.FullName)
                try { $null = $fs.Read($head, 0, 3) } finally { $fs.Dispose() }
                if (-not ($head[0] -eq 0xEF -and $head[1] -eq 0xBB -and $head[2] -eq 0xBF)) {
                    $missingBom += $_.Name
                }
            }
        if ($missingBom.Count -gt 0) {
            Write-Host "        no UTF-8 BOM: $($missingBom -join ', ')" -ForegroundColor Red
            Write-Host "        Windows PowerShell 5.1 misdecodes these and cannot parse them." -ForegroundColor Yellow
            $global:LASTEXITCODE = 1
            return
        }
        $global:LASTEXITCODE = 0
    }

    Gate 'go vet' { go vet ./... }
    Gate 'go test' { go test ./... -count=1 }

    # The market is a nested module, so the two commands above do not descend into
    # it. Both halves run in ONE gate: they are the same claim ("the market's Go
    # compiles and its tests pass"), and splitting them would let a vet failure be
    # read as a test failure in the summary.
    Gate 'plugin market go vet + test' {
        Push-Location (Join-Path $root 'tools/plugin-market')
        try {
            go vet ./...
            if ($LASTEXITCODE -ne 0) { return }
            go test ./... -count=1
        } finally { Pop-Location }
    }

    # Both frontends, in one gate, run in sequence. `npm run build` in the wrong
    # directory silently builds the other app, so each one is pushed into
    # explicitly and the FIRST failure stops the gate rather than being overwritten
    # by the second build's exit code.
    Gate 'frontend builds' {
        Push-Location (Join-Path $root 'frontend')
        try {
            npm run build
            if ($LASTEXITCODE -ne 0) { return }
        } finally { Pop-Location }
        Push-Location (Join-Path $root 'tools/plugin-market/frontend')
        try { npm run build } finally { Pop-Location }
        if ($LASTEXITCODE -ne 0) { return }

        # The control center IS the market's interface (see tools/build-all.ps1), and its
        # build has to land in `frontend/dist` BEFORE the wails build gate below: that Go
        # binary embeds `frontend/dist`, so a copy made afterwards would ship the PREVIOUS
        # interface and look exactly like "the change did nothing". Those four steps are
        # duplicated from build-all.ps1 on purpose - a gate has to be runnable on its own,
        # and a gate that depends on someone having run a build script first is not a gate.
        Push-Location (Join-Path $root 'tools/plugin-market/cc')
        try {
            npm run build
            if ($LASTEXITCODE -ne 0) { return }
        } finally { Pop-Location }
        $ccDist = Join-Path $root 'tools/plugin-market/cc/dist'
        $embedDist = Join-Path $root 'tools/plugin-market/frontend/dist'
        if (-not (Test-Path (Join-Path $ccDist 'index.html'))) {
            Write-Host "        the control center's build produced no index.html: $ccDist" -ForegroundColor Red
            $global:LASTEXITCODE = 1
            return
        }
        if (Test-Path $embedDist) { Remove-Item -Recurse -Force $embedDist }
        Copy-Item -Recurse -Force $ccDist $embedDist
        Write-Host "        copied the control center's build into frontend\dist (the tree the embed reads)" -ForegroundColor DarkGray
    }

    # The control center is a third app, and until this gate existed NOTHING type-checked
    # it: `wails build` proves it compiles (vite strips types) and build-all.ps1 proves it
    # packages, so a page reading `cfg.PetScale` when the host sends `petScale` was green
    # everywhere and wrong on screen. Two halves, because they answer different questions:
    # svelte-check says "every field exists on the type", and the jsdom render says "the
    # page printed the host's numbers" - the second one is what the screenshots were for.
    Gate 'control center checks' {
        Push-Location (Join-Path $root 'tools/plugin-market/cc')
        try {
            npm run check
            if ($LASTEXITCODE -ne 0) { return }
            npm run smoke
        } finally { Pop-Location }
    }

    Gate 'static wiring checks' { node (Join-Path $PSScriptRoot 'check-frontend.cjs') }

    # The plugins' own front ends have their own test suite, and it is a DIFFERENT
    # kind of check from everything above: the static checks read the plugin
    # directories as text, and the harness runs the PET's page against a fake
    # backend. Neither ever executes a plugin's app.js, so neither can see the
    # plugin's own logic - which line it picks for a phase, whether a switch it
    # saved comes back, whether a refused capability breaks its interface, or
    # whether the headless frame and the workshop stay in their own appearances.
    #
    # It runs in the MARKET repo because that is where the plugins live (market/ is
    # a separate git repository this one ignores). A missing working copy is a SKIP
    # rather than a failure: the host repo alone is a legitimate checkout, and
    # blaming the user for a repository the host repo does not contain would make
    # this gate unusable outside the maintainer's tree.
    Gate 'plugin front-end tests' {
        $marketDir = Join-Path $root 'market'
        if (-not (Test-Path (Join-Path $marketDir 'package.json'))) {
            Write-Host "market/ is not checked out here; skipping the plugin tests." -ForegroundColor Yellow
            $global:LASTEXITCODE = 0
            return
        }
        Push-Location $marketDir
        try {
            if (-not (Test-Path (Join-Path $marketDir 'node_modules'))) {
                npm install 2>&1 | Out-Null
                if ($LASTEXITCODE -ne 0) { return }
            }
            npx vitest run
        } finally { Pop-Location }
    }

    Gate 'behaviour harness' {
        Remove-Item -Recurse -Force (Join-Path $PSScriptRoot 'harness-profile') -ErrorAction SilentlyContinue
        node (Join-Path $PSScriptRoot 'run-frontend-harness.cjs')
    }

    Gate 'wails build' {
        # Both Wails programs, because the pixel gate below launches the MARKET's
        # exe as well as the pet's, and a gate that reports "MISSING EXECUTABLE"
        # for a program this very script forgot to build would be blaming the user
        # for the script's omission. The market builds first, then the pet, which
        # keeps the pet's `wails build -s` (the one that regenerates the frontend
        # bindings the static checks read) last.
        Push-Location (Join-Path $root 'tools/plugin-market')
        try {
            wails build -s
            if ($LASTEXITCODE -ne 0) { return }
        } finally { Pop-Location }
        wails build -s
        if ($LASTEXITCODE -ne 0) { return }

        # Copy the market next to the pet, exactly as tools/build-all.ps1 does.
        #
        # This is not tidiness and it was a real defect report: the pet's
        # "open the plugin market" button launches `<dir of PetPC.exe>\PetPCPluginMarket.exe`,
        # while `wails build -s` inside tools/plugin-market writes only to
        # `tools/plugin-market/build/bin/`. So a green `wails build` gate with a stale
        # copy in build\bin is a run where the NEW market code was never started -
        # the user sees the OLD program's opinion about a NEW manifest, and (in the
        # case that prompted this) an installed plugin rendered as "manifest cannot be
        # parsed" because the old binary did not know one of its permissions.
        #
        # Doing it here rather than leaving it to build-all.ps1 means the gate that
        # PROVES the build is also the step that makes the built program reachable.
        $builtMarket = Join-Path $root 'tools\plugin-market\build\bin\PetPCPluginMarket.exe'
        $petBin = Join-Path $root 'build\bin'
        if (Test-Path -LiteralPath $builtMarket) {
            New-Item -ItemType Directory -Force -Path $petBin | Out-Null
            $target = Join-Path $petBin 'PetPCPluginMarket.exe'
            try {
                Copy-Item -LiteralPath $builtMarket -Destination $target -Force -ErrorAction Stop
                Write-Host "        copied PetPCPluginMarket.exe -> build\bin" -ForegroundColor DarkGray
            } catch {
                # A LOCKED target is the ordinary case - the market window is open - and
                # it must not be silent. A copy that quietly fails leaves build\bin
                # holding the OLD program, which is precisely the state that reported a
                # freshly packaged plugin as "manifest cannot be parsed" (the old binary
                # did not know its permissions). So this fails the gate, with the fix.
                Write-Host "        FAILED to refresh build\bin\PetPCPluginMarket.exe: $($_.Exception.Message)" -ForegroundColor Red
                Write-Host "        关掉正在运行的「插件市场」窗口后重跑；否则桌宠启动的仍是旧程序。" -ForegroundColor Yellow
                $global:LASTEXITCODE = 1
            }
        }
    }

    # These launch the freshly built PetPC.exe or PetPCPluginMarket.exe, drive
    # them with synthetic mouse input and screenshot the desktop, so they must come
    # after the build. They need an interactive desktop; a headless/session-0 run
    # will report that rather than a code failure.
    #
    # They are launched as CHILD PROCESSES in Windows PowerShell rather than
    # dot-invoked in this one, and that is not a preference: each one compiles a
    # C# helper with `Add-Type -ReferencedAssemblies 'System.Drawing'`, which is
    # the Windows PowerShell idiom. Under PowerShell 7 that call REPLACES the
    # default reference set, so the helpers fail to compile with CS0234
    # ("System.Threading" / "System.Drawing" not found) and the gate dies before
    # it can even check whether there is a desktop. Running them in the
    # interpreter they are written for is what makes the verdict be about the code.
    #
    # `-NoProfile -ExecutionPolicy Bypass` matches how these scripts document their
    # own invocation. The exit code propagates through $LASTEXITCODE like any other
    # native command, which is what Gate() reads.
    $ps = Join-Path $env:SystemRoot 'System32\WindowsPowerShell\v1.0\powershell.exe'
    if (-not (Test-Path $ps)) { $ps = 'powershell.exe' }

    # --- is there a desktop that can host a WebView2 window? ------------------
    #
    # Asked ONCE, before the pixel gates, by doing the thing they all depend on:
    # starting the freshly built pet and looking for a window with a real area.
    #
    # The probe is needed because the four gates do not agree on how to report this
    # environment's limit. verify-live / verify-plugin-surface / verify-plugin-market
    # detect it themselves and `exit 0` with an explanation. verify-region.ps1 does
    # not - it `throw`s "exited (1)" - and it is FROZEN by this change's own task
    # list, precisely so that a gate which has already been proven cannot have its
    # verdict silently redefined. So the coordination belongs here, in the script
    # that runs all of them: when the desktop cannot host a WebView2 window, the
    # three gates that would report "NO INTERACTIVE DESKTOP" are reported as
    # SKIP, and no gate is blamed for a condition the OS refuses to provide.
    #
    # Failure to create the controller is NOT a defect of this build: it was
    # reproduced with the pre-change binary from before this feature existed
    # ("error creating controller with 800700aa: The requested resource is in use"),
    # which is the same evidence README's "已知限制" section records.
    $desktopProbe = @'
$ErrorActionPreference = 'Stop'
Add-Type -TypeDefinition @"
using System;
using System.Runtime.InteropServices;
using System.Text;
public class DP {
  [DllImport("user32.dll")] public static extern bool EnumWindows(EnumProc cb, IntPtr p);
  public delegate bool EnumProc(IntPtr h, IntPtr p);
  [DllImport("user32.dll")] public static extern uint GetWindowThreadProcessId(IntPtr h, out uint pid);
  [DllImport("user32.dll", CharSet=CharSet.Unicode)] public static extern int GetClassName(IntPtr h, StringBuilder s, int n);
  [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr h, out RECT r);
  [StructLayout(LayoutKind.Sequential)] public struct RECT { public int Left, Top, Right, Bottom; }
  public static uint Pid;
  public static bool Found;
  public static IntPtr Find(uint pid) {
    IntPtr found = IntPtr.Zero;
    EnumWindows((h,p) => {
      uint wpid; GetWindowThreadProcessId(h, out wpid);
      if (wpid != pid) return true;
      var sb = new StringBuilder(256); GetClassName(h, sb, sb.Capacity);
      if (sb.ToString() == "wailsWindow") { found = h; return false; }
      return true;
    }, IntPtr.Zero);
    return found;
  }
  public static bool HasArea(uint pid) {
    IntPtr h = Find(pid);
    if (h == IntPtr.Zero) return false;
    RECT r; GetWindowRect(h, out r);
    return (r.Right-r.Left) > 0 && (r.Bottom-r.Top) > 0;
  }
}
"@
$exe = $args[0]
$data = $args[1]
$env:PETPC_DATA_DIR = $data
$p = Start-Process -FilePath $exe -PassThru
$ok = $false
for ($i=0; $i -lt 120; $i++) {
    Start-Sleep -Milliseconds 100
    if ($p.HasExited) { break }
    if ([DP]::HasArea([uint32]$p.Id)) { $ok = $true; break }
}
if (-not $p.HasExited) { $p.Kill() }
if ($ok) { exit 0 } else { exit 1 }
'@

    $probeFile = Join-Path $env:TEMP 'petpc-desktop-probe.ps1'
    [System.IO.File]::WriteAllText($probeFile, $desktopProbe, (New-Object System.Text.UTF8Encoding($false)))
    $probeData = Join-Path $env:TEMP 'petpc-desktop-probe-data'
    Remove-Item -Recurse -Force $probeData -ErrorAction SilentlyContinue
    New-Item -ItemType Directory -Force -Path $probeData | Out-Null
    & $ps -NoProfile -ExecutionPolicy Bypass -File $probeFile (Join-Path $root 'build\bin\PetPC.exe') $probeData *> $null
    $desktopUsable = ($LASTEXITCODE -eq 0)
    Remove-Item $probeFile -Force -ErrorAction SilentlyContinue

    function PixelGate([string] $script) {
        if (-not $desktopUsable) {
            Write-Host "NO INTERACTIVE DESKTOP: skipped." -ForegroundColor Yellow
            Write-Host "A WebView2 controller could not be created in this session, and this"
            Write-Host "was reproduced with the PRE-CHANGE binary, so it is an environment limit"
            Write-Host "rather than a defect of this build. Same convention as the other gates."
            $script:skipped += $script
            $global:LASTEXITCODE = 0
            return
        }
        & $ps -NoProfile -ExecutionPolicy Bypass -File (Join-Path $PSScriptRoot $script)
    }
    Gate 'region pixel checks (real window)' { PixelGate 'verify-region.ps1' }
    Gate 'live overlay pixel checks (real window)' { PixelGate 'verify-live.ps1' }
    Gate 'plugin surface pixel checks (real window)' { PixelGate 'verify-plugin-surface.ps1' }
    Gate 'plugin market window checks (real window)' { PixelGate 'verify-plugin-market.ps1' }
} finally {
    Pop-Location
}

Write-Host ""
Write-Host "=== summary ===" -ForegroundColor Cyan
$failed = 0
foreach ($r in $results) {
    if ($r.Ok) {
        Write-Host ("  PASS  " + $r.Gate) -ForegroundColor Green
    } else {
        Write-Host ("  FAIL  " + $r.Gate) -ForegroundColor Red
        $failed++
    }
}
if ($skipped.Count -gt 0) {
    Write-Host ""
    Write-Host ("  SKIP  " + $skipped.Count + " pixel gate(s) - no interactive desktop:") -ForegroundColor Yellow
    foreach ($s in $skipped) { Write-Host ("          " + $s) -ForegroundColor Yellow }
    Write-Host "        (Each gate reports the same limit itself when it runs; this run" -ForegroundColor Yellow
    Write-Host "         could not host a WebView2 window at all, reproduced with the" -ForegroundColor Yellow
    Write-Host "         pre-change binary. Not counted as failures.)" -ForegroundColor Yellow
}
Write-Host ""
if ($failed -eq 0) {
    Write-Host "ALL GATES PASSED" -ForegroundColor Green
} else {
    Write-Host "$failed GATE(S) FAILED" -ForegroundColor Red
    exit 1
}

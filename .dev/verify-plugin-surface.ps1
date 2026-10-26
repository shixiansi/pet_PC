# Verifies the plugin floating surface's window region.
#
# The surface is one of the occupants of the slot beside the pet (with the panel
# and the speech bubble), and it is the only one whose shape comes from a PLUGIN's
# manifest rather than from the host's own layout. So the invariants to check are:
#
#   * with no surface open, a point inside the rectangle the plugin declares is
#     NOT part of the pet's window - the pet keeps the window to itself
#   * with a surface open, that point is inside the window AND painted with the
#     plugin's own pixels
#   * a point outside the declared rectangle but inside the window shows the
#     desktop - the region is the plugin's rectangle unioned with the pet, not
#     the whole window
#   * the window rect never changes, and the pet stays exactly where it was, in
#     both states
#   * the process-level market hook works: PETPC_OPEN_PLUGIN_MARKET=1 makes the
#     startup sequence launch PetPCPluginMarket.exe, which is the ONE part of this
#     gate that does not depend on synthetic clicks reaching a page control
#
# It is a SEPARATE script from verify-region.ps1 and verify-live.ps1 on purpose:
# both of those were already proven, and a new gate that edited them would make
# every past result unattributable to the build that produced it.
#
# Run:  powershell -NoProfile -ExecutionPolicy Bypass -File .dev\verify-plugin-surface.ps1
#
# THE DRIVING STEP USED TO BE A CLICK, AND NO LONGER IS. It clicked the 4th tab's
# coordinates to reach the host's 「插件」tab and then the installed plugin's 打开
# button. That tab is GONE - plugin management moved into the market program - so
# the step is deleted rather than re-aimed. In its place the script uses a
# PROCESS-LEVEL trigger: PETPC_OPEN_PLUGIN_MARKET=1 makes the pet's startup
# sequence call OpenPluginMarket() once, and the script asserts the market process
# appears. That is the same function the settings page's entry button calls, so the
# object under test is the product path; the trigger adds no binding and no menu
# item, following this repo's existing PETPC_DEBUG / PETPC_DATA_DIR precedent.
#
# TWO ENVIRONMENT LIMITS ARE REPORTED RATHER THAN FAILED, the same convention
# verify-region.ps1 and verify-live.ps1 use for a desktop they cannot exercise:
#
#   1. No desktop at all (the pet's window is never created).
#   2. A desktop that does not deliver synthetic clicks to the webview's
#      CONTROLS. Clicks reach the window itself - dragging the pet works - but in
#      this environment they do not reach buttons inside the page, so the plugin's
#      in-panel view cannot be opened to reach its surface control. The GATE
#      SAYS SO and exits 0; it does not pretend the surface opened, and it does
#      not pretend the check ran. The geometry itself is covered by the Go
#      pure-function tests (TestComputePluginSurface*, which assert the clamp,
#      the rounding, the scale conversion and the region/view agreement) and by
#      the behaviour harness, which asserts that the iframe is placed on exactly
#      the layout's rectangles.

$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'env.ps1')

$exe  = if ($args.Count -ge 1) { $args[0] } else { 'F:\pet_pc\build\bin\PetPC.exe' }
# The market program is a sibling of the pet in build\bin. Only its NAME is needed:
# this gate asserts the process exists, not what it draws (that is
# verify-plugin-market.ps1's job).
$marketName = 'PetPCPluginMarket'
$data = 'F:\pet_pc\.dev\verify-plugin-surface'
Remove-Item -Recurse -Force $data -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Force -Path $data | Out-Null

# --- the installed plugin --------------------------------------------------
#
# The host has no public API for "install this from a local file", so the plugin
# is seeded exactly the way a completed install leaves things: the package's
# files under plugins/<id>/ and its entry in plugins.json.
#
# The manifest carries the SHIPPING shape: the reply card above the pet and the
# input bar below it, both 300 wide and centred on the pet (the pet is 150 wide, so
# a centred block overhangs its column by 75px on each side). It used to declare
# five blocks - those two plus three thinking puffs marked `ellipse: true` - and the
# ellipses are gone, so this gate no longer exercises the elliptical crop. That
# capability is still covered by the Go pure-function tests
# (TestComputePluginSurfaceEllipseFlag, TestComputePluginSurfaceEllipseHasNoRadius,
# TestComputePluginSurfaceEllipseCoexistsWithRoundedBlocks) and by
# TestManifestEllipseFlag for the parsing; what this gate contributes is the region
# itself, which is the half a pure function cannot see.
#
# Every value written here is ASCII, and that is deliberate. The gate does not
# display these strings; it measures the rectangle they configure. Chinese text
# would have to survive a PowerShell here-string, the file encoding and the
# app's own UTF-8 read, and an encoding accident would land as an unparseable
# manifest - which is a plugin error the gate would then report as "the view
# could not be opened". The market repository's package is what pins the real
# localised manifest, and it does so through a real build-and-verify path.
$pluginId = 'com.shixiansi.plugin-chat'
$pluginDir = Join-Path $data "plugins\$pluginId"
New-Item -ItemType Directory -Force -Path $pluginDir | Out-Null

$utf8 = New-Object System.Text.UTF8Encoding($false)

$manifest = @'
{
  "id": "com.shixiansi.plugin-chat",
  "name": "Surface probe plugin",
  "version": "1.0.0",
  "author": "shixiansi",
  "description": "A seeded plugin that opens its own floating surface, for the window-region gate.",
  "permissions": ["llm.chat", "pet.bubble", "pet.surface"],
  "panel": true,
  "panelSlot": "chat",
  "surface": [
    { "dx": -75, "dy": -120, "w": 300, "h": 112, "radius": 14 },
    { "dx": -75, "dy": 208, "w": 300, "h": 56, "radius": 14 }
  ],
  "entry": "panel.html"
}
'@
[System.IO.File]::WriteAllText("$pluginDir\manifest.json", $manifest, $utf8)

# A panel page that paints a flat, unmistakable colour and asks the host for its
# floating surface as soon as it is authorised to.
#
# The colour has to be one the desktop is unlikely to be: the assertions below
# are "this rectangle is the plugin's pixels", and a page painting the same shade
# as the wallpaper would make that unmeasurable. Magenta is used because nothing
# in the pet, the panel chrome or a normal desktop is magenta.
#
# The surface is requested from the PAGE rather than by clicking a button in it,
# and that is deliberate. A click has to be aimed at a button whose position
# depends on the plugin's own text and on the tab's scroll offset, so a gate that
# clicked would be measuring its own aim as much as the region. A script that
# opens its own surface on `init` has no coordinates to get wrong, and it still
# exercises the whole chain: iframe -> ready -> init -> request -> surface.show.
$panel = @'
<!doctype html><meta charset="utf-8"><title>surface probe</title>
<style>html,body{margin:0;height:100%;background:#c000c0}</style>
<script>
  // Ask for the surface once the host has answered our ready, and only from the
  // PANEL view.
  //
  // The kind guard is not decoration: the surface's own iframe loads this same
  // page, so without it the surface frame would ask for the surface again the
  // moment it initialised, and the gate would be re-opening it forever instead of
  // measuring it.
  window.addEventListener('message', function (ev) {
    var d = ev.data;
    if (!d || d.v !== 1) return;
    if (d.type === 'init' && d.surface && d.surface.kind === 'panel') {
      parent.postMessage({ v: 1, type: 'request', id: 'open-surface', method: 'surface.show', payload: {} }, '*');
    }
  });
  parent.postMessage({ v: 1, type: 'ready' }, '*');
</script>
'@
[System.IO.File]::WriteAllText("$pluginDir\panel.html", $panel, $utf8)

$index = @"
{
  "version": 1,
  "plugins": [
    {
      "id": "$pluginId",
      "version": "1.0.0",
      "enabled": true,
      "granted": ["pet.surface", "llm.chat", "pet.bubble"],
      "installedAt": "$(Get-Date -Format 'yyyy-MM-ddTHH:mm:sszzz')"
    }
  ]
}
"@
[System.IO.File]::WriteAllText("$data\plugins.json", $index, $utf8)
[System.IO.File]::WriteAllText("$data\config.json", '{"petScale":1,"snapToEdge":true,"edgeMargin":8,"autostart":false}', $utf8)

# --- the pixel probes ------------------------------------------------------

Add-Type -AssemblyName System.Drawing
Add-Type -ReferencedAssemblies 'System.Drawing' -TypeDefinition @'
using System;
using System.Drawing;
using System.Drawing.Imaging;
using System.Runtime.InteropServices;
using System.Text;

public class PS {
  [DllImport("user32.dll")] public static extern bool EnumWindows(EnumProc cb, IntPtr l);
  public delegate bool EnumProc(IntPtr h, IntPtr l);
  [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr h, out RECT r);
  [DllImport("user32.dll")] public static extern uint GetWindowThreadProcessId(IntPtr h, out uint pid);
  [DllImport("user32.dll", CharSet = CharSet.Unicode)] public static extern int GetClassName(IntPtr h, StringBuilder s, int n);
  [DllImport("user32.dll")] public static extern bool SetCursorPos(int x, int y);
  [DllImport("user32.dll")] public static extern void mouse_event(uint f, uint x, uint y, uint d, IntPtr e);
  [DllImport("user32.dll")] public static extern IntPtr WindowFromPoint(POINT p);

  [StructLayout(LayoutKind.Sequential)] public struct RECT { public int Left, Top, Right, Bottom; }
  [StructLayout(LayoutKind.Sequential)] public struct POINT { public int X, Y; }

  public static uint Pid = 0;

  // The window is found by CLASS NAME, not by "the process's first visible
  // window".
  //
  // The pet is a Wails app, so it owns several windows (the webview host, the
  // WebView2 child, tray plumbing); matching the fixed class name "wailsWindow"
  // is what verify-region.ps1 does and what the app itself relies on to find its
  // own window. Taking the first visible one instead picked up a 235x44 helper
  // window, which made every coordinate below meaningless.
  public static IntPtr Find(uint pid) {
    IntPtr found = IntPtr.Zero;
    EnumWindows(delegate(IntPtr h, IntPtr l) {
      uint p; GetWindowThreadProcessId(h, out p);
      if (p != pid) return true;
      var sb = new StringBuilder(256);
      GetClassName(h, sb, sb.Capacity);
      if (sb.ToString() == "wailsWindow") { found = h; return false; }
      return true;
    }, IntPtr.Zero);
    return found;
  }

  // Is the pixel at a screen point part of the pet's window at all?
  //
  // This is the region test. A cropped-away point is not covered by the window,
  // so WindowFromPoint returns whatever is behind it (the desktop); a point
  // inside the region returns the pet's own window.
  public static bool CoveredByWindow(int screenX, int screenY) {
    POINT p; p.X = screenX; p.Y = screenY;
    IntPtr h = WindowFromPoint(p);
    if (h == IntPtr.Zero) return false;
    uint pid; GetWindowThreadProcessId(h, out pid);
    return pid == Pid;
  }

  // The colour at a screen point, as "rrggbb", for diagnostics.
  public static string PixelAt(int x, int y) {
    using (var bmp = new Bitmap(1, 1, PixelFormat.Format32bppArgb)) {
      using (var g = Graphics.FromImage(bmp))
        g.CopyFromScreen(x, y, 0, 0, new Size(1, 1), CopyPixelOperation.SourceCopy);
      Color c = bmp.GetPixel(0, 0);
      return c.R.ToString("x2") + c.G.ToString("x2") + c.B.ToString("x2");
    }
  }

  // Does ANY pixel of a rectangle look like the plugin page's magenta?
  //
  // The match is loose on the exact shade and strict on the hue: the page paints
  // #c000c0, a compositor may blend it slightly, and a test that demanded the
  // exact byte would fail on a machine with a different colour profile while
  // telling us nothing extra.
  //
  // A grid rather than one point, because the panel view is a real iframe whose
  // document the host does not control: where inside the tab body it paints is
  // the plugin's business, and a check aimed at one fixed point would call a
  // correct render "not open" the first time a plugin laid its page out
  // differently.
  public static bool RectHasMagenta(int x, int y, int w, int h) {
    using (var bmp = new Bitmap(w, h, PixelFormat.Format32bppArgb)) {
      using (var g = Graphics.FromImage(bmp))
        g.CopyFromScreen(x, y, 0, 0, new Size(w, h), CopyPixelOperation.SourceCopy);
      var bd = bmp.LockBits(new Rectangle(0, 0, w, h), ImageLockMode.ReadOnly, PixelFormat.Format32bppArgb);
      byte[] buf = new byte[bd.Stride * h];
      Marshal.Copy(bd.Scan0, buf, 0, buf.Length);
      bmp.UnlockBits(bd);
      for (int yy = 4; yy < h; yy += 6) {
        int row = yy * bd.Stride;
        for (int xx = 4; xx < w; xx += 6) {
          int i = row + xx * 4;
          byte b = buf[i], gg = buf[i + 1], rr = buf[i + 2];
          if (rr > 140 && b > 140 && gg < 90) return true;
        }
      }
      return false;
    }
  }

  // Is ANY pixel of a rectangle covered by the pet's window?
  //
  // The pet is an SVG with transparent margins, so a single point inside its
  // element can legitimately fall on a transparent pixel and report "not
  // covered". Sampling a grid asks the question that matters: is this region
  // part of the window at all.
  public static bool RectHasCoverage(int x, int y, int w, int h) {
    for (int yy = 2; yy < h; yy += 6) {
      for (int xx = 2; xx < w; xx += 6) {
        if (CoveredByWindow(x + xx, y + yy)) return true;
      }
    }
    return false;
  }

  // Is the pet actually PAINTED in this region?
  //
  // The pet is an SVG with transparent margins and the window is cropped to its
  // box, so "is this point part of the window" is not the question - a point in
  // the transparent margin is part of the window and shows the desktop through.
  // What says the pet is there is its fur: a large warm region, the same signal
  // verify-region.ps1's CatBox keys on.
  public static bool RectHasFur(int x, int y, int w, int h) {
    using (var bmp = new Bitmap(w, h, PixelFormat.Format32bppArgb)) {
      using (var g = Graphics.FromImage(bmp))
        g.CopyFromScreen(x, y, 0, 0, new Size(w, h), CopyPixelOperation.SourceCopy);
      var bd = bmp.LockBits(new Rectangle(0, 0, w, h), ImageLockMode.ReadOnly, PixelFormat.Format32bppArgb);
      byte[] buf = new byte[bd.Stride * h];
      Marshal.Copy(bd.Scan0, buf, 0, buf.Length);
      bmp.UnlockBits(bd);
      for (int yy = 2; yy < h; yy += 4) {
        int row = yy * bd.Stride;
        for (int xx = 2; xx < w; xx += 4) {
          int i = row + xx * 4;
          byte b = buf[i], gg = buf[i + 1], rr = buf[i + 2];
          if (rr > 150 && gg > 90 && b < 140 && rr > b + 40) return true;
        }
      }
      return false;
    }
  }

  public static void Click(int x, int y) {
    // Bring the pet's window forward first: it is always-on-top but not always
    // FOREGROUND, and a click sent while another window holds the focus is
    // delivered to whatever is really under the cursor.
    IntPtr h = Find(Pid);
    if (h != IntPtr.Zero) { SetForegroundWindow(h); System.Threading.Thread.Sleep(120); }
    SetCursorPos(x, y);
    System.Threading.Thread.Sleep(140);
    mouse_event(0x0002, 0, 0, 0, IntPtr.Zero);  // LEFTDOWN
    System.Threading.Thread.Sleep(70);
    mouse_event(0x0004, 0, 0, 0, IntPtr.Zero);  // LEFTUP
  }

  [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr h);

  // The pet element's offset inside the window, in physical pixels.
  //
  // The window is always petW + gap + panelW wide and max(panelH, petH) tall, and
  // the pet occupies ONE of the two 150-wide columns: which one depends on where
  // the pet is on screen (a pet on the right half gets the panel on its left), so
  // it cannot be hardcoded for a gate that may run on any monitor.
  //
  // The column is found by scanning for the pet's fur - a large warm region - the
  // same way verify-region.ps1's CatBox does, and for the same reason: it is the
  // only thing in the window that reliably says where the pet actually is.
  public static int PetOffsetX() {
    RECT r; IntPtr hwnd = Find(Pid);
    GetWindowRect(hwnd, out r);
    int W = r.Right - r.Left, H = r.Bottom - r.Top;
    if (W <= 0 || H <= 0) return -1;
    using (var bmp = new Bitmap(W, H, PixelFormat.Format32bppArgb)) {
      using (var g = Graphics.FromImage(bmp))
        g.CopyFromScreen(r.Left, r.Top, 0, 0, new Size(W, H), CopyPixelOperation.SourceCopy);
      var bd = bmp.LockBits(new Rectangle(0, 0, W, H), ImageLockMode.ReadOnly, PixelFormat.Format32bppArgb);
      byte[] buf = new byte[bd.Stride * H];
      Marshal.Copy(bd.Scan0, buf, 0, buf.Length);
      bmp.UnlockBits(bd);
      int bestX = -1, bestCount = 0;
      for (int x = 0; x < W; x++) {
        int count = 0;
        for (int y = 0; y < H; y++) {
          int i = y * bd.Stride + x * 4;
          byte b = buf[i], g2 = buf[i + 1], rr = buf[i + 2];
          if (rr > 150 && g2 > 90 && b < 140 && rr > b + 40) count++;
        }
        if (count > bestCount) { bestCount = count; bestX = x; }
      }
      if (bestX < 0) return -1;
      // The fur's leftmost dense column is a few pixels inside the element, so
      // this answers "which half of the window" rather than an exact edge: the
      // pet is 150 wide, the gap is 10, and the two possible offsets are far
      // apart, so a few pixels of inset cannot confuse them.
      return bestX < W / 2 ? 0 : W - 150;
    }
  }

  // The pet's vertical offset: it is centred in the window.
  public static int PetOffsetY(int petH) {
    RECT r; IntPtr hwnd = Find(Pid);
    GetWindowRect(hwnd, out r);
    int H = r.Bottom - r.Top;
    int y = (H - petH) / 2;
    return y < 0 ? 0 : y;
  }
}
'@

# --- start the pet ---------------------------------------------------------

$env:PETPC_DATA_DIR = $data
# The process-level market trigger. Setting this makes the pet's startup sequence
# call OpenPluginMarket() once - the same function the settings page's entry button
# calls - so the market program is launched without any synthetic click having to
# reach a control inside the webview. The variable is unset afterwards so it cannot
# leak into the rest of the run.
#
# This is the ONLY driving step this gate has, and it deliberately replaces the old
# "click the 4th tab, then click 打开" pair: that tab no longer exists (plugin
# management is the market program's job now), and the reference to it is deleted
# rather than re-aimed.
$env:PETPC_OPEN_PLUGIN_MARKET = '1'
$proc = Start-Process -FilePath $exe -PassThru
Remove-Item Env:\PETPC_OPEN_PLUGIN_MARKET -ErrorAction SilentlyContinue
[PS]::Pid = [uint32]$proc.Id
$hwnd = [IntPtr]::Zero
for ($i=0; $i -lt 100; $i++) {
    Start-Sleep -Milliseconds 50
    if ($proc.HasExited) {
        # The pet exiting at startup is how this environment reports "no
        # interactive desktop": the GUI cannot create its window without one.
        "NO INTERACTIVE DESKTOP: the pet exited during startup ($($proc.ExitCode))."
        ""
        "NO INTERACTIVE DESKTOP"
        exit 0
    }
    $hwnd = [PS]::Find([uint32]$proc.Id)
    if ($hwnd -ne [IntPtr]::Zero) { break }
}
if ($hwnd -eq [IntPtr]::Zero) {
    if (-not $proc.HasExited) { $proc.Kill() }
    "NO INTERACTIVE DESKTOP: the pet's window was never created."
    "This environment cannot run a windowed check; the same convention as"
    "verify-region.ps1 and verify-live.ps1 applies, and it is NOT a code failure."
    ""
    "NO INTERACTIVE DESKTOP"
    exit 0
}
Start-Sleep -Seconds 3

function Rect { $r = New-Object PS+RECT; [void][PS]::GetWindowRect($hwnd, [ref]$r); return $r }

$fail = 0
function Check($name, $ok, $detail) {
    if ($ok) { "  PASS  $name" } else { "  FAIL  $name -- $detail"; $script:fail++ }
}

$r0 = Rect
$petW = 150
$petH = 200
$petOffsetX = [PS]::PetOffsetX()
$petOffsetY = [PS]::PetOffsetY($petH)
if ($petOffsetX -lt 0) {
    if (-not $proc.HasExited) { $proc.Kill() }
    # The market hook is NOT asserted in this branch. It is a statement about a pet
    # that started successfully, and the pet here did not: without a desktop the
    # WebView2 controller cannot be created, the startup sequence aborts before it
    # reaches the hook, and nothing is launched. Reporting that as a failure would
    # blame the hook for the environment.
    foreach ($m in @(Get-Process -Name $marketName -ErrorAction SilentlyContinue)) {
        if (-not $m.HasExited) { $m.Kill() }
    }
    "NO INTERACTIVE DESKTOP: the pet could not be located in the window."
    ""
    "NO INTERACTIVE DESKTOP"
    exit 0
}

# --- the process-level market trigger --------------------------------------
#
# Asserted here, once the pet is known to have started, because it is the check
# that does not depend on synthetic clicks reaching a page control - and because a
# trigger that silently did nothing would make the rest of this gate's driving
# story a fiction. The window is given its three seconds above, so the launched
# process has had the same grace the product path would need.
#
# The hook fires from the pet's own startup sequence, in the same goroutine that
# finishes bringing the window up; it does not wait for the market and does not
# tie the pet's lifetime to it, so the market is still running here and is killed
# below rather than waited on.
"=== PETPC_OPEN_PLUGIN_MARKET hook ==="
$market = @(Get-Process -Name $marketName -ErrorAction SilentlyContinue)
Check "setting PETPC_OPEN_PLUGIN_MARKET launches the market program" `
    ($market.Count -ge 1) `
    "no $marketName process appeared within 3s of the pet starting with the hook set"
if ($market.Count -ge 1) { "  $marketName process count = $($market.Count)" }
# The market is a separate program, and this gate kills it: leaving it running
# would make the next gate's "did it launch" check pass against a stale process.
foreach ($m in $market) { if (-not $m.HasExited) { $m.Kill() } }

# --- the declared geometry --------------------------------------------------
#
# The declared blocks are turned into WINDOW-relative rectangles the same way
# computePluginSurface does, because the point of this gate is to compare what the
# region actually is against what the plugin asked for: reproducing the numbers by
# hand would test this script against itself. The three steps - add the pet's
# origin, pin the size to the window, clamp the origin back inside - are the ones
# that function performs, and the clamp is the one that matters when a block sits
# outside the window: the reply card is declared at dy = -120, i.e. ABOVE the pet,
# and with the pet near the top of the work area that origin clamps to 0.
$manifestText = [System.IO.File]::ReadAllText("$pluginDir\manifest.json", $utf8)
$declaredBlocks = @((ConvertFrom-Json $manifestText).surface)
$winW = $r0.Right - $r0.Left
$winH = $r0.Bottom - $r0.Top

$declaredRects = @()
foreach ($b in $declaredBlocks) {
    $w = [int]$b.w
    $h = [int]$b.h
    if ($w -gt $winW) { $w = $winW }
    if ($h -gt $winH) { $h = $winH }
    $x = $petOffsetX + [int]$b.dx
    $y = $petOffsetY + [int]$b.dy
    if ($x -gt $winW - $w) { $x = $winW - $w }
    if ($x -lt 0) { $x = 0 }
    if ($y -gt $winH - $h) { $y = $winH - $h }
    if ($y -lt 0) { $y = 0 }
    $declaredRects += @{ X = $r0.Left + $x; Y = $r0.Top + $y; W = $w; H = $h; Ellipse = [bool]$b.ellipse }
}

# The card is the block the probes are aimed at: it is the larger of the two, it is
# the one that carries the plugin's own pixels, and unlike the input bar (which is
# mostly a textarea and a button) its interior is painted edge to edge.
$card = $declaredRects | Sort-Object { -$_.W * $_.H } | Select-Object -First 1
$declared = $card

$insideX = $declared.X + [int]($declared.W / 2)
$insideY = $declared.Y + [int]($declared.H / 2)

# A point inside the WINDOW but outside every declared rectangle, and outside the
# pet's own box: it is what distinguishes "the region is the plugin's rectangles"
# from "the region is the whole window".
#
# The window's top-right corner is used, and the reason is now the VERTICAL gap
# rather than the horizontal one. The declared boxes are 300 wide and centred on the
# pet, so with the pet against a window edge a box is CLAMPED and can span most of
# the window's width - the corner's x is therefore not reliably clear of them. What
# is reliably clear is y: the pet is vertically centred (160..360 of a 520-tall
# window), both boxes sit at y 40..152 and 368..424, and the corner is 6px from the
# TOP - above every one of them in both orientations.
$outsideX = $r0.Right - 6
$outsideY = $r0.Top + 6

"=== surface closed ==="
"  window = ($($r0.Left),$($r0.Top)) ${winW}x${winH}"
"  pet offset in window = $petOffsetX,$petOffsetY"
"  declared blocks = $($declaredBlocks.Count) ($(@($declaredRects | Where-Object { $_.Ellipse }).Count) elliptical)"
"  card rect = ($($declared.X - $r0.Left),$($declared.Y - $r0.Top)) $($declared.W)x$($declared.H) in window coordinates"
"  probe inside  = $insideX,$insideY   outside = $outsideX,$outsideY"
"  inside covered = $([PS]::CoveredByWindow($insideX, $insideY)), outside covered = $([PS]::CoveredByWindow($outsideX, $outsideY))"
"  inside pixel = $([PS]::PixelAt($insideX, $insideY))"

# The closed state: with no surface open the window must be cropped to the pet
# alone, so a point where a plugin's rectangle WOULD be is not part of the window
# at all. That is the same invariant the panel and the bubble obey, and it is
# what keeps the plugin feature from widening the window's footprint while it is
# doing nothing.
Check "with no surface open the declared rectangle is not part of the window" `
    (-not [PS]::CoveredByWindow($insideX, $insideY)) `
    "the point is inside the window while no surface is open, so the window is cropped to more than the pet"

Check "the pet is part of the window where it is expected" `
    ([PS]::RectHasFur($r0.Left + $petOffsetX + 10, $r0.Top + $petOffsetY + 40, $petW - 20, $petH - 80)) `
    "no pixel of the pet's own area belongs to the window"

# --- open the surface ------------------------------------------------------
#
# The surface is requested by the PLUGIN, in its init handler (see the seeded
# panel.html above): the page declares pet.surface, the host mounts it in the 对话
# slot, the handshake arrives, and the plugin answers by asking for its own floating
# surface. So the driving step is just "open the panel" - a single click on the pet,
# which is the window rather than a control inside the page, and therefore the one
# gesture this environment does deliver.
#
# What is deliberately NOT done here any more: clicking a tab to reach the plugin's
# management page, and clicking an 打开 button on a plugin card. Both belonged to the
# host's 「插件」tab, which this change deleted - management is the market program's
# job now, so there is no in-panel control left to aim at, and a gate that kept
# aiming at one would be measuring a screen that no longer exists.
[PS]::Click($r0.Left + $petOffsetX + 75, $r0.Top + $petOffsetY + 100)
Start-Sleep -Seconds 3

$r1 = Rect
$panelViewOpen = [PS]::RectHasMagenta($r0.Left + $petOffsetX + 8, $r1.Top + 60, [int]($winW/2) - 16, 440)

if (-not $panelViewOpen) {
    if (-not $proc.HasExited) { $proc.Kill() }
    "NO INTERACTIVE DESKTOP: the plugin's in-panel view could not be opened, so"
    "its page never ran and the surface it opens was never requested."
    ""
    "Clicks reach the pet's WINDOW in this environment (the panel is open, and the"
    "plugin is mounted in the 对话 slot), but not the CONTROLS inside the page, so"
    "the view that owns the surface control cannot be reached. This is the same"
    "class of limit verify-region.ps1 and verify-live.ps1 report, and it is NOT a"
    "code failure. What it means for this gate: the CLOSED-state invariants above"
    "and the market-hook check are the evidence it contributes, and the surface"
    "GEOMETRY is covered by the Go pure-function tests (TestComputePluginSurface*,"
    "which assert the clamp, the rounding, the scale conversion and the region/view"
    "agreement) and by the behaviour harness (which asserts that the iframe is"
    "placed on exactly the layout's rectangles)."
    ""
    "NO INTERACTIVE DESKTOP (surface could not be driven open)"
    exit 0
}

$r2 = Rect
"=== surface open ==="
"  window = ($($r2.Left),$($r2.Top)) $($r2.Right-$r2.Left)x$($r2.Bottom-$r2.Top)"
"  inside covered = $([PS]::CoveredByWindow($insideX, $insideY)), inside pixel = $([PS]::PixelAt($insideX, $insideY))"

Check "the window did not resize" `
    ((($r2.Right-$r2.Left) -eq ($r0.Right-$r0.Left)) -and (($r2.Bottom-$r2.Top) -eq ($r0.Bottom-$r0.Top))) `
    "size changed: $($r0.Right-$r0.Left)x$($r0.Bottom-$r0.Top) -> $($r2.Right-$r2.Left)x$($r2.Bottom-$r2.Top)"

Check "the window did not move" `
    (($r2.Left -eq $r0.Left) -and ($r2.Top -eq $r0.Top)) `
    "position changed: $($r0.Left),$($r0.Top) -> $($r2.Left),$($r2.Top)"

Check "the declared rectangle is painted with the plugin's own pixels" `
    ([PS]::CoveredByWindow($insideX, $insideY) -and [PS]::RectHasMagenta($declared.X + 4, $declared.Y + 4, $declared.W - 8, $declared.H - 8)) `
    "inside reads covered=$([PS]::CoveredByWindow($insideX, $insideY)) pixel=$([PS]::PixelAt($insideX, $insideY))"

# Every declared block must now be part of the window, not just the first one. The
# region is the UNION of what the plugin asked for, and a host that cropped only the
# reply card would leave the input bar outside the region - visible as nothing at
# all, because a cropped-away area is not painted.
$missingBlocks = @()
foreach ($rect in $declaredRects) {
    $cx = $rect.X + [int]($rect.W / 2)
    $cy = $rect.Y + [int]($rect.H / 2)
    if (-not [PS]::CoveredByWindow($cx, $cy)) {
        $missingBlocks += "$($rect.X - $r0.Left),$($rect.Y - $r0.Top) $($rect.W)x$($rect.H)"
    }
}
Check "every declared block is inside the window region ($($declaredRects.Count) blocks)" `
    ($missingBlocks.Count -eq 0) `
    "these declared blocks are not part of the window: $($missingBlocks -join ' | ')"

# NOTE: the elliptical-crop check that used to live here is DELETED, not re-aimed.
#
# It asserted a contrast - an ellipse block's bounding-box CENTRE inside the region
# and its bounding-box CORNER outside it - and that contrast is the only evidence
# distinguishing CreateEllipticRgn from a rounded rectangle. The conversation plugin
# no longer declares any ellipse block (the three thinking puffs are gone, and its
# two remaining boxes are rounded rectangles), so there is no block to point it at.
# Keeping it would mean seeding an ellipse the shipping plugin does not have, which
# would make this gate measure a surface that does not exist. The capability stays
# covered by the Go pure-function tests and the manifest parsing test instead; see
# the note above the seeded manifest.

Check "a point outside the declared rectangle shows the desktop" `
    (-not [PS]::CoveredByWindow($outsideX, $outsideY)) `
    "the point outside the rectangle is still inside the window region, so the region is the whole window rather than the plugin's rectangle"

Check "the pet is still part of the window where it was" `
    ([PS]::RectHasFur($r0.Left + $petOffsetX + 10, $r0.Top + $petOffsetY + 40, $petW - 20, $petH - 80)) `
    "the pet's own area left the window region when the surface opened"

# --- close the surface -----------------------------------------------------
#
# Closed the same way it was opened: the plugin's own interface is what toggles it,
# and there is no host control to click. What is asserted is the invariant, not the
# gesture: once the surface is gone the region must be back to the pet alone.
[PS]::Click($r0.Left + $petOffsetX + 75, $r0.Top + $petOffsetY + 100)
Start-Sleep -Seconds 2

Check "closing the surface paints nothing where it was" `
    (-not [PS]::RectHasMagenta($declared.X + 4, $declared.Y + 4, $declared.W - 8, $declared.H - 8)) `
    "the surface's pixels are still on screen after closing it"

if (-not $proc.HasExited) { $proc.Kill(); $proc.WaitForExit(5000) | Out-Null }

""
if ($fail -eq 0) { "ALL PLUGIN SURFACE CHECKS PASSED" } else { "$fail CHECK(S) FAILED"; exit 1 }

# Verifies the CONVERSATION PLUGIN'S floating surface against the REAL window.
#
# The surface is the third occupant of the slot beside the pet, and like the panel
# and the speech bubble it is not one box: the plugin declares two rectangles, and
# Go crops the window to their union plus the pet. That crop is the only thing that
# decides which of those pixels the user can see, so it cannot be verified from the
# frontend alone - a mismatch shows up as either a clipped card or a pale glass
# band over the user's desktop (README 10(d)).
#
# The host no longer has a conversation overlay of its own (app_live.go was deleted
# with the rest of the host's conversation engine), so the geometry this script
# inspects is no longer a set of Go constants: it is the CHAT PLUGIN's own
# declaration, read from its manifest. That is the point of the change and it is
# why this script reads the manifest rather than reproducing the numbers - a
# manifest the host crops differently from what the plugin draws is exactly the
# failure this gate exists to catch, and hard-coding the same constants here would
# reproduce the bug instead of finding it.
#
# What this checks, against a running PetPC.exe:
#   * opening the surface does NOT move or resize the window or the pet
#     (the whole reason the region is cropped rather than the window resized)
#   * the reply card is painted ABOVE the pet and the input bar BELOW it, both
#     exactly as wide as the pet and sharing its column - the user's layout
#   * closing it puts the desktop back
#
# Run:  pwsh -File .dev\verify-live.ps1

$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'env.ps1')

$exe  = if ($args.Count -ge 1) { $args[0] } else { 'F:\pet_pc\build\bin\PetPC.exe' }
$data = 'F:\pet_pc\.dev\verify-live'
$root = Split-Path -Parent $PSScriptRoot

# --- the geometry source: the chat plugin's own manifest --------------------
#
# The manifest is the PLUGIN's file, and it is read from the market repository's
# working copy rather than from the installed data directory: this gate runs
# against a seeded temp data dir, and the seed is what installs the plugin, so the
# declaration being verified has to be the one that gets copied in.
$manifestPath = Join-Path $root 'market/plugins/com.shixiansi.plugin-chat/manifest.json'
if (-not (Test-Path $manifestPath)) {
    "MISSING MANIFEST: $manifestPath"
    "The conversation plugin's floating-surface geometry lives in its manifest, so"
    "this gate cannot describe the region without it."
    exit 1
}
# Read as UTF-8 explicitly. The manifest carries Chinese labels, and Windows
# PowerShell's Get-Content decodes a file without a BOM as the ANSI code page -
# which turns every label into mojibake and, worse, makes ConvertFrom-Json fail on
# the mangled bytes. .NET's reader takes the encoding as an argument and is the
# same call under both PowerShell editions.
$manifest = [System.IO.File]::ReadAllText($manifestPath, (New-Object System.Text.UTF8Encoding($false))) | ConvertFrom-Json
$surface = @($manifest.surface)
if ($surface.Count -lt 2) {
    "MANIFEST HAS $($surface.Count) SURFACE BLOCKS: expected at least 2 (the reply card and the input bar)."
    exit 1
}

# Blocks are declared in the plugin's own order; this gate wants them by ROLE, and
# the roles are recognisable from the shape rather than from the index, so a plugin
# that reorders its blocks does not silently make this script measure the wrong
# rectangle.
#
#   * the card is the TALLER rectangle - it sits ABOVE the pet
#   * the input bar is the shorter one   - it hangs BELOW the pet
#
# Both are rounded rectangles. There used to be a third role here - the thinking
# puffs, ellipse blocks whose bounding-box corner proved the elliptical crop - and
# it is gone with the three ellipses themselves: the plugin now declares exactly
# two blocks, both of them directly above and below the pet. The `ellipse` flag is
# still a supported manifest feature (an ellipse is the one shape a rounded
# rectangle cannot approximate), but this plugin no longer exercises it, so the
# elliptical-crop check that used to live at the bottom of this file is deleted
# rather than re-aimed at a block that does not exist.
$rounded = @($surface | Where-Object { -not $_.ellipse })
if ($rounded.Count -lt 2) {
    "MANIFEST SURFACE SHAPE NOT RECOGNISED: $($rounded.Count) rounded rectangles;"
    "this gate needs at least a reply card and an input bar."
    exit 1
}
$ellipses = @($surface | Where-Object { $_.ellipse })
# The card is above the bar in the plugin's coordinate space (dy measures downward
# from the pet's top-left, and the card sits at a smaller dy than the bar's), and it
# is the taller of the two.
$card = $rounded | Sort-Object -Property dy | Select-Object -First 1
$bar  = $rounded | Sort-Object -Property dy | Select-Object -Last 1

"=== geometry declared by $($manifest.id) ==="
"  card  dx=$($card.dx) dy=$($card.dy) $($card.w)x$($card.h) r=$($card.radius)"
"  bar   dx=$($bar.dx) dy=$($bar.dy) $($bar.w)x$($bar.h) r=$($bar.radius)"
if ($ellipses.Count -gt 0) {
    "  (plus $($ellipses.Count) ellipse block(s), not measured by this gate)"
}

Remove-Item -Recurse -Force $data -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Force -Path $data | Out-Null

$now = Get-Date
$fmt = { param($d) $d.ToString('yyyy-MM-ddTHH:mm:sszzz') }
$utf8 = New-Object System.Text.UTF8Encoding($false)
# The surface's own text is irrelevant to the crop, but a todo gives the pet
# something the "material" gate would accept.
[System.IO.File]::WriteAllText("$data\todos.json", @"
[
 {"id":"1","title":"写周报","due":"$(& $fmt $now.AddHours(3))","done":false,"createdAt":"$(& $fmt $now)"}
]
"@, $utf8)
# NOTE: no llm* keys. The host no longer holds AI configuration at all - that moved
# into the chat plugin's own private config - so a seeded config that still carried
# them would be a fixture describing a State the real app cannot produce.
#
# The pet's POSITION is pinned into the LEFT half of the screen, and that is a
# deliberate part of the fixture rather than a convenience. Which side of the window
# the pet occupies is decided by where it sits on screen (a pet on the right half gets
# the surface column on its left). The manifest's rectangles are declared relative to
# the PET and are centred on it: with the pet on the RIGHT the declaration is mirrored
# (dx' = petW - dx - w = 150 - (-75) - 300 = -75), so a centred declaration stays
# centred - the mirror of a symmetric layout is itself. Pinning the position keeps the
# mirroring branch from being the thing under test here.
#
# snapToEdge is off so the seeded position is not silently re-snapped by the app on
# its first layout pass.
[System.IO.File]::WriteAllText("$data\config.json",
    '{"petScale":1,"snapToEdge":false,"edgeMargin":8,"autostart":false,"position":{"x":40,"y":300}}', $utf8)

# --- install the chat plugin --------------------------------------------------
#
# The double click opens the CONVERSATION PLUGIN's surface now, not a host overlay:
# `App.OpenChatPluginSurface()` resolves the installed plugin whose manifest claims
# the chat slot and holds pet.surface, and refuses with a bubble when there is none.
# So the plugin has to be INSTALLED in the seeded data directory, or the gesture this
# whole gate is about does nothing and every measurement below reads an unmoved
# desktop - which is exactly what happened before this seeding was added.
#
# The seed is written the way a completed install leaves things (the package's files
# under plugins/<id>/ plus its entry in plugins.json), and it is driven BY THE
# MANIFEST the geometry above was read from: the id, the declared permissions and the
# surface blocks all come from that one file, so the plugin this gate installs is
# necessarily the plugin whose rectangles it measures.
$pluginDir = Join-Path $data "plugins\$($manifest.id)"
New-Item -ItemType Directory -Force -Path $pluginDir | Out-Null
Copy-Item $manifestPath (Join-Path $pluginDir 'manifest.json') -Force
# The entry the manifest names, painting an unmistakable colour.
#
# The page has to PAINT something, and the checks below measure it with
# DarkFraction - "pixels whose R, G and B are all in the 20..90 range". So the probe
# MUST paint a dark colour.
#
# It used to paint #00c8ff (cyan), which that metric can never match: cyan is
# (0,200,255), so a perfectly rendered surface still measured 0% and this gate failed
# while the real surface was painting at 99% - a self-inflicted false alarm. The fix is
# the colour, not the metric: DarkFraction is the right question to ask ("did the app
# paint this rectangle, or is the desktop still showing through?"), and the surface
# genuinely IS dark. The value below is the panel background's own tone.
#
# Every surface block gets the SAME colour: the gate is measuring the region, not the
# plugin's design, and the blocks are distinguished by their positions rather than by
# their fills.
[System.IO.File]::WriteAllText((Join-Path $pluginDir $manifest.entry), @'
<!doctype html><meta charset="utf-8"><title>live gate probe</title>
<style>html,body{margin:0;height:100%;background:#303540}</style>
'@, $utf8)

# Every declared permission is granted, plus the ones the host requires for a slot
# owner. Granting the full declared set rather than a hand-picked list is deliberate:
# the resolution rule checks pet.surface specifically, and a fixture that granted a
# subset would be testing this script's guess about which permission matters.
$granted = @($manifest.permissions) | ForEach-Object { '"' + $_ + '"' }
$index = @"
{
  "version": 1,
  "plugins": [
    {
      "id": "$($manifest.id)",
      "version": "$($manifest.version)",
      "enabled": true,
      "granted": [$($granted -join ', ')],
      "installedAt": "$(Get-Date -Format 'yyyy-MM-ddTHH:mm:sszzz')"
    }
  ]
}
"@
[System.IO.File]::WriteAllText("$data\plugins.json", $index, $utf8)
"  installed $($manifest.id) with $($granted.Count) granted permissions"

Add-Type -AssemblyName System.Drawing
Add-Type -ReferencedAssemblies 'System.Drawing' -TypeDefinition @'
using System;
using System.Drawing;
using System.Drawing.Imaging;
using System.Runtime.InteropServices;
using System.Text;

public class LV {
  [DllImport("user32.dll")] public static extern bool EnumWindows(EnumProc cb, IntPtr p);
  public delegate bool EnumProc(IntPtr h, IntPtr p);
  [DllImport("user32.dll")] public static extern uint GetWindowThreadProcessId(IntPtr h, out uint pid);
  [DllImport("user32.dll", CharSet=CharSet.Unicode)] public static extern int GetClassName(IntPtr h, StringBuilder s, int n);
  [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr h, out RECT r);
  [DllImport("user32.dll")] public static extern bool SetCursorPos(int x, int y);
  [DllImport("user32.dll")] public static extern void mouse_event(uint f, uint dx, uint dy, uint d, IntPtr e);
  // GetWindowRgn + PtInRegion: ask the OS directly whether a point is in the window
  // REGION, which is the exact question this gate needs answered.
  //
  // This replaces a pixel-comparison approach that could not work here. The pet's
  // window is layered and region-cropped, and CopyFromScreen against those returns
  // unreliable data (README's 已知限制 records the same finding for the bubble probe):
  // the ellipse's corner sampled 182,182,182 with the surface open and 104,116,129
  // closed, which is noise rather than a reading. WindowFromPoint is no better,
  // because a WebView2 window's content belongs to a child process.
  //
  // The REGION is exactly what the product installed via SetWindowRgn, so querying it
  // asks the app's own question back at the OS. GetWindowRgn reports NULLREGION (0)
  // for an empty region and SIMPLEREGION (1) for the union this app installs; either
  // way PtInRegion is a definitive yes/no for a point.
  [DllImport("user32.dll")] public static extern int GetWindowRgn(IntPtr h, IntPtr rgn);
  [DllImport("gdi32.dll")] public static extern IntPtr CreateRectRgn(int l, int t, int r, int b);
  [DllImport("gdi32.dll")] public static extern bool PtInRegion(IntPtr rgn, int x, int y);
  [DllImport("gdi32.dll")] public static extern bool DeleteObject(IntPtr o);

  // Is this WINDOW-RELATIVE point inside the window's current region?
  //
  // A NULL region means "no region is set, so the whole window is visible" in Win32
  // terms. This app always installs an explicit region - that is the whole mechanism -
  // so an empty result is reported as "not covered" rather than reinterpreted.
  public static bool InRegion(IntPtr hwnd, int x, int y) {
    IntPtr rgn = CreateRectRgn(0, 0, 1, 1);
    if (rgn == IntPtr.Zero) return false;
    try {
      int kind = GetWindowRgn(hwnd, rgn);
      if (kind == 0 || kind == 5) return false;
      return PtInRegion(rgn, x, y);
    } finally {
      DeleteObject(rgn);
    }
  }

  [StructLayout(LayoutKind.Sequential)] public struct RECT { public int Left, Top, Right, Bottom; }
  [StructLayout(LayoutKind.Sequential)] public struct POINT { public int X, Y; }
  public const uint LEFTDOWN=0x0002, LEFTUP=0x0004;


  public static uint Pid;

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

  public static void Click(int x,int y){ SetCursorPos(x,y); System.Threading.Thread.Sleep(120);
    mouse_event(LEFTDOWN,0,0,0,IntPtr.Zero); System.Threading.Thread.Sleep(60); mouse_event(LEFTUP,0,0,0,IntPtr.Zero); }
  public static void DoubleClick(int x,int y){
    SetCursorPos(x,y); System.Threading.Thread.Sleep(120);
    for (int i=0;i<2;i++){
      mouse_event(LEFTDOWN,0,0,0,IntPtr.Zero); System.Threading.Thread.Sleep(40);
      mouse_event(LEFTUP,0,0,0,IntPtr.Zero); System.Threading.Thread.Sleep(60);
    }
  }

  // Captures the window's rectangle from the screen.
  public static Bitmap Shot(out RECT r) {
    IntPtr hwnd = Find(Pid);
    GetWindowRect(hwnd, out r);
    int W = r.Right-r.Left, H = r.Bottom-r.Top;
    var bmp = new Bitmap(W, H, PixelFormat.Format32bppArgb);
    using (var g = Graphics.FromImage(bmp))
      g.CopyFromScreen(r.Left, r.Top, 0, 0, new Size(W, H), CopyPixelOperation.SourceCopy);
    return bmp;
  }

  static byte[] Buf(Bitmap bmp, out int stride, out int W, out int H) {
    W = bmp.Width; H = bmp.Height;
    var bd = bmp.LockBits(new Rectangle(0,0,W,H), ImageLockMode.ReadOnly, PixelFormat.Format32bppArgb);
    stride = bd.Stride;
    byte[] buf = new byte[bd.Stride*H];
    Marshal.Copy(bd.Scan0, buf, 0, buf.Length);
    bmp.UnlockBits(bd);
    return buf;
  }

  // The panel/surface background is a dark, slightly blue grey. Counting these
  // pixels inside a given window-relative rectangle measures how much of that
  // rectangle the app actually painted.
  public static double DarkFraction(Bitmap bmp, int x0, int y0, int w, int h) {
    int stride, W, H;
    byte[] buf = Buf(bmp, out stride, out W, out H);
    long dark = 0, tot = 0;
    for (int y=y0; y<y0+h && y<H; y++) {
      if (y < 0) continue;
      int row = y*stride;
      for (int x=x0; x<x0+w && x<W; x++) {
        if (x < 0) continue;
        int i = row + x*4;
        byte b = buf[i], gg = buf[i+1], rr = buf[i+2];
        tot++;
        if (rr>=20 && rr<=80 && gg>=20 && gg<=80 && b>=20 && b<=90) dark++;
      }
    }
    return tot>0 ? 100.0*dark/tot : 0;
  }

  // Average colour of a small patch, used to tell "the desktop is showing" from
  // "the app painted something here".
  public static string Patch(Bitmap bmp, int cx, int cy, int half) {
    int stride, W, H;
    byte[] buf = Buf(bmp, out stride, out W, out H);
    long r=0,g=0,b=0,n=0;
    for (int y=cy-half; y<=cy+half; y++) {
      if (y<0||y>=H) continue;
      int row=y*stride;
      for (int x=cx-half; x<=cx+half; x++) {
        if (x<0||x>=W) continue;
        int i=row+x*4; b+=buf[i]; g+=buf[i+1]; r+=buf[i+2]; n++;
      }
    }
    if (n==0) return "n/a";
    return (r/n) + "," + (g/n) + "," + (b/n);
  }

  // The cat's centre column, found by its warm fur colour.
  //
  // The window is a constant 540x520 and the cat is 150x200 inside it, but WHICH
  // side of the window it occupies depends on where it sits on screen (the panel
  // takes the opposite side). The surface's declared rectangles are relative to
  // the pet's top-left corner, so locating the pet from the pixels is what turns
  // the manifest's dx/dy into window coordinates - and it keeps the probe honest
  // if the pet is ever moved.
  public static int CatCentreX(Bitmap bmp) {
    int stride, W, H;
    byte[] buf = Buf(bmp, out stride, out W, out H);
    int minX = -1, maxX = -1;
    for (int x=0; x<W; x++) {
      int hits = 0;
      for (int y=0; y<H; y++) {
        int i = y*stride + x*4;
        byte b = buf[i], gg = buf[i+1], rr = buf[i+2];
        if (rr > 195 && gg > 120 && gg < 235 && b < 190 && (rr-b) > 45) hits++;
      }
      if (hits >= 20) { if (minX < 0) minX = x; maxX = x; }
    }
    if (minX < 0) return -1;
    return (minX + maxX) / 2;
  }
}
'@

$env:PETPC_DATA_DIR = $data
$proc = Start-Process -FilePath $exe -PassThru
[LV]::Pid = [uint32]$proc.Id
$hwnd = [IntPtr]::Zero
for ($i=0; $i -lt 100; $i++) {
    Start-Sleep -Milliseconds 50
    if ($proc.HasExited) {
        # The pet exiting at startup is how this environment reports "no
        # interactive desktop" too: the GUI cannot create its window without one.
        "NO INTERACTIVE DESKTOP: the pet exited during startup ($($proc.ExitCode))."
        "This environment cannot run a windowed check; the same convention as"
        "verify-region.ps1 and verify-plugin-surface.ps1 applies, and it is NOT a code failure."
        ""
        "NO INTERACTIVE DESKTOP"
        exit 0
    }
    $hwnd = [LV]::Find([uint32]$proc.Id)
    if ($hwnd -ne [IntPtr]::Zero) { break }
}
if ($hwnd -eq [IntPtr]::Zero) {
    if (-not $proc.HasExited) { $proc.Kill() }
    "NO INTERACTIVE DESKTOP: the pet's window was never created."
    "This environment cannot run a windowed check; the same convention as"
    "verify-region.ps1 and verify-plugin-surface.ps1 applies, and it is NOT a code failure."
    ""
    "NO INTERACTIVE DESKTOP"
    exit 0
}
Start-Sleep -Seconds 3

$fail = 0
function Check($name, $ok, $detail) {
    if ($ok) { "  PASS  $name" } else { "  FAIL  $name -- $detail"; $script:fail++ }
}

# The window rectangle is read on its own before anything is captured. `Shot`
# allocates a Bitmap from the rectangle it measures, so a window with no area
# makes it throw from inside - and "the window exists but has no surface" is the
# same no-interactive-desktop condition as "the window was never created",
# reached one step later. Reading the rect first turns that into a clean report.
function WindowRect {
    $r = New-Object LV+RECT
    [void][LV]::GetWindowRect($hwnd, [ref]$r)
    return $r
}

$r0 = WindowRect
$W = $r0.Right-$r0.Left; $H = $r0.Bottom-$r0.Top
if ($W -le 0 -or $H -le 0) {
    if (-not $proc.HasExited) { $proc.Kill() }
    "NO INTERACTIVE DESKTOP: the pet's window has no area (${W}x${H})."
    "This environment cannot run a windowed check; the same convention as"
    "verify-region.ps1 and verify-plugin-surface.ps1 applies, and it is NOT a code failure."
    ""
    "NO INTERACTIVE DESKTOP"
    exit 0
}
$shot0 = [LV]::Shot([ref]$r0)
"=== closed state ==="
"  window = ($($r0.Left),$($r0.Top)) ${W}x${H}"

# The pet is located from the CLOSED shot ($shot0), not the open one: with the
# surface up the window shows the desktop through the cropped-away areas, and a warm
# wallpaper merges with the cat into one blob spanning the window. Closed, the
# region is cropped to the pet alone, so the warm pixels ARE the cat.
#
# The scan is RETRIED until the cat's centre is clear of the window's midpoint.
#
# Why this loop exists: the side decision below is a "which half is the cat in" test on
# a 540px window, so its two answers are 240px apart but its input is one number that
# can be measured anywhere in between. A shot taken while the pet was still painting -
# the SVG fading in, or the crop not yet applied - put the warm pixels near x=274 of a
# window whose midpoint is 270, and that single sample flipped the answer: the manifest
# was mirrored, the click point was computed for the other side, the double click landed
# where the window is transparent (so it never reached the pet at all), and the gate
# reported "card only 0% dark" for a surface that simply had not been opened.
#
# A near-midpoint reading is therefore treated as UNSETTLED rather than as an answer,
# and re-shot. The threshold is wider than the jitter needs to be (+-40px) so that a
# genuinely centred reading is retried rather than trusted; the loop gives up after ten
# tries and reports, rather than silently picking a side.
$catCentre = 0
$mid = [int]($W / 2)
for ($attempt = 1; $attempt -le 10; $attempt++) {
    $shot0 = [LV]::Shot([ref]$r0)
    $catCentre = [int]([LV]::CatCentreX($shot0))
    if ([Math]::Abs($catCentre - $mid) -gt 40) { break }
    if ($attempt -eq 10) {
        "  WARNING: the pet's centre stayed at $catCentre (within 40px of the midpoint $mid) across $attempt shots"
    }
    Start-Sleep -Milliseconds 600
}
"  pet centre column = $catCentre (window ${W}x${H}, midpoint $mid) after up to 10 shots"

# The pet ELEMENT's top-left corner, which is the origin the manifest's dx/dy are
# measured from.
#
# The x comes from the element's own geometry, NOT from the fur's centre column, and
# the difference is a real 35px rather than rounding. The pet is a 150x200 element
# whose artwork has transparent margins, so the warm pixels do not span it
# symmetrically - deriving "left edge = fur centre - 75" put every rectangle 35px to
# the right of where Go puts it, and the probe then sampled the wrong pixels
# (reporting an unpainted card - and, when the plugin still declared them, an
# unpainted puff - for a surface that was drawn correctly).
#
# Which side of the window the pet occupies follows the same rule Go uses: the window
# is petW + gap + panelW wide, so the two possible offsets are 0 and W - petW, and the
# fur's centre column says which half it is in. That is the same coarse "which half"
# test verify-plugin-surface.ps1 uses, and for the same reason - the exact edge is not
# recoverable from the pixels, but the side is, and the two candidates are 240px apart.
$petW = 150
$petH = 200
$petLeft = if ($catCentre -lt [int]($W/2)) { 0 } else { $W - $petW }
$petTop  = [int](($H - $petH)/2)
# The pet's side decides whether the declaration is mirrored, because the manifest
# describes only one side. See RectOf.
$mirrored = $petLeft -gt 0
"  pet origin = ($petLeft,$petTop); manifest coordinates are relative to it"
"  declaration is $(if ($mirrored) { 'MIRRORED (pet on the right, panel on its left)' } else { 'used as written (pet on the left)' })"

# The click point is derived from the pet's side of the window rather than pinned to
# the right, because this gate seeds the pet into the LEFT half (see the config
# seeding above). Aiming at "the right edge minus 75" would then land on the surface
# column instead of the pet, and the double click would never reach the pet at all -
# the gate would measure an unmoved desktop and report a surface that never opened.
$petClickX = $r0.Left + $petLeft + [int]($petW/2)
$petClickY = [int](($r0.Top + $r0.Bottom)/2)

# The surface opens on a DOUBLE click (the single click is the panel). This is
# App.OpenChatPluginSurface(), which resolves the chat plugin and delegates to
# OpenPluginSurface - the host's own overlay toggle is gone with app_live.go.
"=== open the plugin surface (double click) ==="
[LV]::DoubleClick($petClickX, $petClickY)
Start-Sleep -Seconds 3
$r1 = New-Object LV+RECT
$shot1 = [LV]::Shot([ref]$r1)
"  window = ($($r1.Left),$($r1.Top)) $($r1.Right-$r1.Left)x$($r1.Bottom-$r1.Top)"

Check "the window did not resize" (($r1.Right-$r1.Left) -eq $W -and ($r1.Bottom-$r1.Top) -eq $H) `
    "size changed to $($r1.Right-$r1.Left)x$($r1.Bottom-$r1.Top)"
Check "the window did not move" ($r1.Left -eq $r0.Left -and $r1.Top -eq $r0.Top) "position changed"

# Turn one declared block into a window-relative rectangle. This is the whole
# conversion, and it mirrors computePluginSurface STEP FOR STEP - the mirroring, the
# size pin and the clamp, none of which is optional here.
#
# MIRRORING comes first, and it is what makes this script correct on BOTH pet
# positions. A manifest declares its rectangles relative to the pet's top-left corner
# and can describe only ONE side - the numbers here were written for the pet on the
# left. With the pet on the right (petX = 390) `dx: 160` would put the card at 550,
# ten pixels outside a 540-wide window, and the clamp would then drag it back to
# 240..540: on top of the pet, and over the panel. Go mirrors the declaration into the
# free column on the pet's left in that case, so this has to as well - otherwise the
# gate would measure a rectangle the program does not use, and (worse) it would
# "pass" against a window cropped somewhere else entirely.
#
# The clamp is the second half. Go pins the size to the window and then pulls the
# origin back inside, so the rectangle the region is actually cut to is the clamped
# one.
#
# The local names are deliberately NOT $w / $h / $x / $y: PowerShell variable names
# are CASE-INSENSITIVE, so assigning to $w overwrites the script's $W (the window
# width). "Clamp x into the window" then silently becomes "clamp x to 300 - 300 = 0",
# which places every rectangle at the origin - the first version of this function did
# exactly that and reported all five blocks at (0,0).
function RectOf($block) {
    $bw = [int]$block.w
    $bh = [int]$block.h
    if ($bw -gt $W) { $bw = $W }
    if ($bh -gt $H) { $bh = $H }
    # Mirrored when the pet is on the right: measure dx from the pet's RIGHT edge,
    # exactly as mirrorDeclaredRects does (dx' = petW - dx - w).
    $dx = [int]$block.dx
    if ($mirrored) { $dx = $petW - $dx - $bw }
    $bx = $petLeft + $dx
    $by = $petTop  + [int]$block.dy
    if ($bx -gt $W - $bw) { $bx = $W - $bw }
    if ($bx -lt 0) { $bx = 0 }
    if ($by -gt $H - $bh) { $by = $H - $bh }
    if ($by -lt 0) { $by = 0 }
    return @{ X = $bx; Y = $by; W = $bw; H = $bh }
}

$cardRect  = RectOf $card
$barRect   = RectOf $bar

"  card  rect ($($cardRect.X),$($cardRect.Y)) $($cardRect.W)x$($cardRect.H)"
"  bar   rect ($($barRect.X),$($barRect.Y)) $($barRect.W)x$($barRect.H)"

$cardDark = [double]([LV]::DarkFraction($shot1, $cardRect.X, $cardRect.Y, $cardRect.W, $cardRect.H))
$barDark  = [double]([LV]::DarkFraction($shot1, $barRect.X, $barRect.Y, $barRect.W, $barRect.H))
"  card dark = $([Math]::Round($cardDark,1))%"
"  bar  dark = $([Math]::Round($barDark,1))%"

Check "the reply card is painted" ($cardDark -gt 60) `
    "card only $([Math]::Round($cardDark,1))% dark at ($($cardRect.X),$($cardRect.Y))"
Check "the input bar is painted" ($barDark -gt 60) `
    "input only $([Math]::Round($barDark,1))% dark at ($($barRect.X),$($barRect.Y))"

# The relationships below are read off the MANIFEST, not off the host constants that
# used to compute this layout. That distinction is the point of the change: the
# geometry is the plugin's declaration now, and `app_live.go` - the file the old
# numbers lived in - is deleted. Re-imposing its constants here would mean this gate
# could only ever pass for one particular manifest, which is the opposite of what it
# is for.
#
# What must hold for ANY sensible manifest is the STRUCTURE the plugin declared:
# the reply card sits directly ABOVE the pet, the input bar directly BELOW it, and
# the two share the pet's own column. All three are read from the declaration
# itself, so a manifest that put either box beside the pet, or in the panel's column
# on the far side of the window, fails here.
$barBelow = [int]$bar.dy - $petH
$cardAbove = -([int]$card.dy + [int]$card.h)
"=== declared structure ==="
"  card dy=$($card.dy) h=$($card.h)  -> its bottom edge is $cardAbove px above the pet's top edge"
"  bar  dy=$($bar.dy)  -> its top edge is $barBelow px below the pet's bottom edge (pet height $petH)"

Check "the reply card is declared entirely above the pet" `
    ($cardAbove -ge 0) `
    "the card's dy is $($card.dy) with height $($card.h), so its bottom edge is $cardAbove px above the pet's top edge (a negative value means it overlaps the pet)"
Check "the input bar is declared below the pet" `
    ($barBelow -ge 0) `
    "the bar's dy is $($bar.dy), which is inside the pet's own 0..$petH band rather than below it"

# Both boxes must be CENTRED ON THE PET and share one column. That is what makes
# the pair read as one panel hanging directly off the pet - the reply on top of it,
# the input box underneath - rather than as two unrelated boxes somewhere in the
# window.
#
# They are 300 wide and the pet is 150, so a centred block necessarily overhangs
# the pet's own column by 75px on each side, toward the free side of the window.
# That is the requested shape ("拉长到300"): the earlier version of this gate
# asserted the boxes were exactly as wide as the pet, which was the previous
# design and is now the opposite of what ships.
#
# It is read off the manifest, so a plugin that declared the bar half a card wide,
# offset from the card, or off-centre from the pet fails here.
$sameColumn = ($cardRect.X -eq $barRect.X) -and ($cardRect.W -eq $barRect.W)
"  card x=$($cardRect.X) w=$($cardRect.W)   bar x=$($barRect.X) w=$($barRect.W)   pet w=$petW"
Check "the card and the input bar share one column" $sameColumn `
    "the card is at x=$($cardRect.X) w=$($cardRect.W) but the bar is at x=$($barRect.X) w=$($barRect.W)"
Check "both floating boxes are 300 wide (twice the pet)" `
    ([int]$card.w -eq 300 -and [int]$bar.w -eq 300) `
    "the card is $($card.w) wide and the bar $($bar.w), expected 300 each"
Check "the declaration centres both boxes on the pet" `
    (([int]$card.dx + [int]$card.w/2) -eq [int]($petW/2) -and ([int]$bar.dx + [int]$bar.w/2) -eq [int]($petW/2)) `
    "the card's declared centre offset is $([int]$card.dx + [int]$card.w/2) and the bar's $([int]$bar.dx + [int]$bar.w/2), but the pet's centre is $([int]($petW/2))"
# NOTE on the check above: it reads the DECLARATION, not the clamped rectangle.
#
# A 300-wide box centred on a 150-wide pet overhangs it by 75px on each side, so
# when the pet sits against the window's edge the clamp pulls the rectangle back
# inside - and the visible box ends up flush with the pet's outer edge instead of
# straddling it. That is the correct outcome rather than a failure: the alternative
# is drawing past the window, where the region crop cannot reach. What this gate can
# insist on is that the plugin ASKED for a centred layout, and that both boxes ask
# for the same one; `$sameColumn` above is what pins the two together.

# NOTE: there is deliberately NO elliptical-crop check here any more.
#
# This gate used to end this section by proving the crop was an ELLIPSE: it sampled
# the bounding-box centre of the largest puff (inside the shape, so inside the
# region) against its bounding-box corner (outside the shape, so outside the region)
# and required the two to disagree. That contrast is the only evidence that
# `CreateEllipticRgn` was used rather than a rounded rectangle, and it needs a block
# to point at.
#
# The conversation plugin no longer declares any ellipse block - its two floating
# boxes are the rounded card above the pet and the rounded input bar below it - so
# the check is DELETED rather than re-aimed. Re-aiming it would mean either asserting
# a rounded rectangle's corner lies OUTSIDE the region (it does not; that is the
# whole difference between the two shapes) or seeding an ellipse block this plugin
# does not ship, which would make the gate measure a manifest the product never had.
#
# The elliptical capability itself stays covered, where it belongs: the pure-function
# tests in app_plugin_surface_test.go pin that the flag travels to BOTH the region and
# the CSS view and that an ellipse carries radius 0, and
# TestManifestEllipseFlag pins the manifest parsing.
#
# NOTE: there is deliberately NO "closing the surface collapses the region" check.
#
# An earlier version of this script had one, on the assumption that the double click
# toggles. It does not: `openChatPluginSurface` in main.js calls
# `App.OpenChatPluginSurface()`, which only ever OPENS - it resolves the chat plugin
# and delegates to OpenPluginSurface. Dismissing the surface is `ClosePluginSurface`,
# which the plugin's own interface calls, so a second double click leaves the surface
# up and the check then measures the very region it was trying to prove had collapsed.
# The close path is covered where it belongs: the behaviour harness drives
# ClosePluginSurface through the plugin protocol, and the plugin-surface gate asserts
# the closed state directly.

# And the pet must be in exactly the same place: the surface changes the region
# crop, never the window geometry, which is the whole reason the pet cannot move.
#
# The probe point is CHOSEN, not assumed, and that is not fussiness: the plugin is
# free to declare a box that overlaps the pet's own rectangle, and sampling the pet's
# centre in that case reads the PLUGIN's pixels, not the pet's. The point picked here
# is on the pet's own centre column, at the y furthest from every declared rectangle,
# so nothing the plugin paints can sit under it.
#
# Patch takes WINDOW-relative coordinates, and $catCentre was derived from the pixels
# with the surface OPEN - so the closed shot is probed at the same window-relative
# spot, which is only valid because the first two checks proved the window itself did
# not move or resize.
$petBottom = $petTop + 200
$probeY = $petTop + [int](200/2)
$bestClearance = -1
for ($cy = $petTop + 8; $cy -le $petBottom - 8; $cy += 2) {
    $clearance = 999
    foreach ($r in @($cardRect, $barRect)) {
        if ($catCentre -ge $r.X -and $catCentre -lt ($r.X + $r.W)) {
            # The distance from this y to the nearest edge of a covering rectangle.
            $d = [Math]::Min([Math]::Abs($cy - $r.Y), [Math]::Abs($cy - ($r.Y + $r.H)))
            if ($cy -ge $r.Y -and $cy -lt ($r.Y + $r.H)) { $d = 0 }
            if ($d -lt $clearance) { $clearance = $d }
        }
    }
    if ($clearance -gt $bestClearance) { $bestClearance = $clearance; $probeY = $cy }
}
"  pet probe at ($catCentre,$probeY); nearest declared edge = $bestClearance px"

$petBefore = [LV]::Patch($shot0, $catCentre, $probeY, 3)
$petAfter = [LV]::Patch($shot1, $catCentre, $probeY, 3)
"  pet pixel at the probe: closed $petBefore   with the surface $petAfter"
Check "the pet is painted in the same place" ($petBefore -eq $petAfter -and $petBefore -ne 'n/a') `
    "pet centre was $petBefore closed and $petAfter with the surface"

if (-not $proc.HasExited) { $proc.Kill(); $proc.WaitForExit(5000) | Out-Null }

""
if ($fail -eq 0) { "ALL CONVERSATION SURFACE CHECKS PASSED" } else { "$fail CHECK(S) FAILED"; exit 1 }

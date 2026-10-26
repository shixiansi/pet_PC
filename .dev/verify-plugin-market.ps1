# Verifies the plugin market's real window: its frame, its size and its layout.
#
# The market is a SEPARATE PROGRAM, and it exists because the pet's panel is a
# fixed 380x520 rectangle the OS crops the window to. Four problems were visible in
# the screenshots that prompted the move, and three of them are LAYOUT problems
# that only a real layout engine at the real window size can settle:
#
#   1. the permission rows were clipped by the detail pane's right edge, because
#      the pane was narrower than the text;
#   2. the address box only showed half of itself and the save button had wrapped
#      onto its own line;
#   3. the conversation and the two lists fought over one scrollbar.
#
# (3) is pinned structurally by the static checks - #detail-pane is a SIBLING of
# #list-pane and each is its own scroll container - and needs no window. (1) and
# (2) are measured here, on the real window.
#
# EVERY COORDINATE BELOW IS CLIENT-RELATIVE, and the capture is of the CLIENT
# rectangle rather than the window's. That is not a detail: Wails' Width/Height are
# the OUTER window's, so a capture of the window includes the frame, and the title
# bar's own colours then dominate any "what is the background here" measurement -
# which is exactly how the first version of this script measured the title bar
# instead of the address row and reported the input as 2px wide.
#
# What this checks, against a running PetPCPluginMarket.exe:
#   * the window has a RESIZABLE frame: WS_THICKFRAME and WS_MAXIMIZEBOX are both
#     set. Without them the window is fixed at whatever size it opened at, and a
#     user cannot widen it to read a long permission explanation.
#   * the configured 980x700 window size is what actually opened
#   * the address input takes the share of its row that `flex: 1 1 auto` promises,
#     and the save button did NOT wrap onto a second line
#   * no text reaches the detail pane's right edge, so none is clipped by it
#
# Run:  powershell -NoProfile -ExecutionPolicy Bypass -File .dev\verify-plugin-market.ps1
#
# NO INTERACTIVE DESKTOP is reported rather than failed, the same convention
# verify-region.ps1 / verify-live.ps1 / verify-plugin-surface.ps1 use: a windowed
# check cannot run in a session without a desktop, and reporting that as a code
# failure would make the gate lie about the build.

$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'env.ps1')

$root = Split-Path -Parent $PSScriptRoot
# The exe is looked for in the same two places the PET looks for it (see
# PluginMarketCandidates): beside the pet in build\bin first, then the market's own
# build output. Both exist in practice - `tools\build-all.ps1` copies it to
# build\bin, while running `wails build -s` inside tools/plugin-market leaves it in
# the module's own tree - and a gate that only accepted one of them would fail on a
# checkout that was built the other way.
$exe = if ($args.Count -ge 1) {
    $args[0]
} else {
    $candidates = @(
        (Join-Path $root 'build\bin\PetPCPluginMarket.exe'),
        (Join-Path $root 'tools\plugin-market\build\bin\PetPCPluginMarket.exe')
    )
    ($candidates | Where-Object { Test-Path $_ } | Select-Object -First 1)
}

if (-not $exe) {
    "MISSING EXECUTABLE: none of"
    "  $root\build\bin\PetPCPluginMarket.exe"
    "  $root\tools\plugin-market\build\bin\PetPCPluginMarket.exe"
    "Build it with:  pwsh -File tools\build-all.ps1"
    exit 1
}

# The market shares the pet's data directory, so the gate points it at a scratch
# one: a run must not read or write the user's real plugin list.
$data = Join-Path $root '.dev\verify-plugin-market'
Remove-Item -Recurse -Force $data -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Force -Path $data | Out-Null
$utf8 = New-Object System.Text.UTF8Encoding($false)

# --- a seeded plugin, so the detail pane has something to lay out -------------
#
# The permission-list check needs a plugin that DECLARES permissions: the market
# opens with nothing selected in an empty data directory, and the detail pane then
# shows its placeholder. A gate that sampled the placeholder would report "no text
# reached the edge" while never having drawn a permission row at all - the vacuous
# pass this seeding exists to prevent.
#
# The declaration is deliberately close to the shipping chat plugin's: seven
# permissions including the long-explanation ones (`net.fetch`, `plugin.config`),
# because those are the rows whose text is wide enough to reach the pane's edge if
# the wrap is not working.
#
# THE NAME AND DESCRIPTION ARE \u-ESCAPED ON PURPOSE, and it is a correctness fix
# rather than tidiness. Windows PowerShell 5.1 reads a .ps1 with no BOM as the
# system ANSI codepage (GBK on this machine), so the Chinese that used to sit in
# this here-string was written to disk as GBK bytes inside a file the loader reads
# as UTF-8. The result was a manifest.json that was NOT VALID JSON: the plugin
# came back with Error="manifest 不是合法的 JSON", the detail pane drew the error
# line instead of any permission rows, and the right-edge check below then passed
# on a pane that had no permission rows in it at all - a vacuous pass, which is
# exactly what the seeding exists to prevent.
#
# Escapes keep this file pure ASCII, so no codepage can corrupt the fixture. The
# decoded value is the same text.
$pluginId = 'com.shixiansi.plugin-chat'
$pluginDir = Join-Path $data "plugins\$pluginId"
New-Item -ItemType Directory -Force -Path $pluginDir | Out-Null
$manifest = @'
{
  "id": "com.shixiansi.plugin-chat",
  "name": "\u5bf9\u8bdd\u63d2\u4ef6",
  "version": "1.1.0",
  "author": "shixiansi",
  "description": "\u5728\u9762\u677f\u91cc\u4e0e\u684c\u5ba0\u65c1\u548c\u5927\u6a21\u578b\u5bf9\u8bdd\u7684\u5b98\u65b9\u63d2\u4ef6\uff1b\u81ea\u5df1\u7684 AI \u914d\u7f6e\u4e0e\u5bc6\u94a5\u7531\u81ea\u5df1\u4fdd\u5b58\u3002",
  "permissions": ["llm.chat", "pet.bubble", "pet.surface", "plugin.config", "storage", "net.fetch", "todos.read"],
  "netHosts": ["api.example.com"],
  "panel": true,
  "panelSlot": "chat",
  "entry": "panel.html"
}
'@
[System.IO.File]::WriteAllText("$pluginDir\manifest.json", $manifest, $utf8)
[System.IO.File]::WriteAllText("$pluginDir\panel.html", '<!doctype html><p>market gate probe</p>', $utf8)
$index = @"
{
  "version": 1,
  "plugins": [
    {
      "id": "$pluginId",
      "version": "1.1.0",
      "enabled": true,
      "granted": ["llm.chat", "pet.bubble", "pet.surface", "plugin.config", "storage"],
      "installedAt": "2026-01-01T00:00:00Z"
    }
  ]
}
"@
[System.IO.File]::WriteAllText("$data\plugins.json", $index, $utf8)
# This program's own settings, so the address box has a value to show rather than
# an empty field - an empty input still draws its border, but the check should
# exercise the case the screenshot complained about (a LONG value in it).
[System.IO.File]::WriteAllText("$data\plugin-market.json",
    '{"marketBaseUrl":"https://gh-proxy.com/https://raw.githubusercontent.com/shixiansi/pet-pc-plugins/main","windowW":980,"windowH":700}', $utf8)

# --- the pixel probes --------------------------------------------------------

Add-Type -AssemblyName System.Drawing
Add-Type -ReferencedAssemblies 'System.Drawing' -TypeDefinition @'
using System;
using System.Drawing;
using System.Drawing.Imaging;
using System.Runtime.InteropServices;
using System.Text;
using System.Collections.Generic;

public class MK {
  [DllImport("user32.dll")] public static extern bool EnumWindows(EnumProc cb, IntPtr p);
  public delegate bool EnumProc(IntPtr h, IntPtr p);
  [DllImport("user32.dll")] public static extern uint GetWindowThreadProcessId(IntPtr h, out uint pid);
  [DllImport("user32.dll", CharSet=CharSet.Unicode)] public static extern int GetClassName(IntPtr h, StringBuilder s, int n);
  [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr h, out RECT r);
  [DllImport("user32.dll")] public static extern bool GetClientRect(IntPtr h, out RECT r);
  [DllImport("user32.dll")] public static extern bool ClientToScreen(IntPtr h, ref POINT p);
  [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr h);
  [DllImport("user32.dll")] public static extern IntPtr GetForegroundWindow();
  [DllImport("user32.dll")] public static extern bool SetWindowPos(IntPtr h, IntPtr after, int x, int y, int cx, int cy, uint flags);
  static readonly IntPtr HWND_TOPMOST = new IntPtr(-1);
  static readonly IntPtr HWND_NOTOPMOST = new IntPtr(-2);
  const uint SWP_NOMOVE = 0x0002, SWP_NOSIZE = 0x0001, SWP_SHOWWINDOW = 0x0040;
  // The window style bits are the ONLY way to tell "the user can resize this" from
  // outside the process: Wails does not expose its own options at runtime, and the
  // difference between a resizable and a fixed window is exactly these two bits.
  [DllImport("user32.dll", EntryPoint="GetWindowLongPtrW")] public static extern IntPtr GetWindowLongPtr(IntPtr h, int i);

  [StructLayout(LayoutKind.Sequential)] public struct RECT { public int Left, Top, Right, Bottom; }
  [StructLayout(LayoutKind.Sequential)] public struct POINT { public int X, Y; }

  public static uint Pid;

  // WS_THICKFRAME is the sizing border; WS_MAXIMIZEBOX is the maximize button.
  public const long WS_THICKFRAME  = 0x00040000L;
  public const long WS_MAXIMIZEBOX = 0x00010000L;
  public const int GWL_STYLE = -16;

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

  public static long Style(IntPtr h) { return (long)GetWindowLongPtr(h, GWL_STYLE); }

  // Raises the window and reports whether it really became the foreground window.
  //
  // This is not politeness. `CopyFromScreen` reads whatever is actually on the
  // screen at those coordinates, so a window that stayed behind another one yields
  // a screenshot of the OTHER window - and the measurements below would then be of
  // something that is not this program at all. The caller verifies the returned
  // flag rather than trusting the call, and treats "could not raise it" as the
  // no-desktop case: a background window is not measurable, in exactly the way a
  // window with no area is not.
  public static void Raise(IntPtr hwnd) {
    SetWindowPos(hwnd, HWND_TOPMOST, 0, 0, 0, 0, SWP_NOMOVE | SWP_NOSIZE | SWP_SHOWWINDOW);
    SetForegroundWindow(hwnd);
    System.Threading.Thread.Sleep(600);
  }

  // Drops the window back out of topmost. Called at the very end, so the gate does
  // not leave a window pinned above everything the user is doing.
  public static void Lower(IntPtr hwnd) {
    SetWindowPos(hwnd, HWND_NOTOPMOST, 0, 0, 0, 0, SWP_NOMOVE | SWP_NOSIZE);
  }

  [DllImport("user32.dll")] public static extern bool PrintWindow(IntPtr h, IntPtr dc, uint flags);
  // PW_RENDERFULLCONTENT makes PrintWindow ask the window to render its OWN content
  // into the supplied DC - including DirectComposition surfaces, which is what
  // WebView2 draws into. Without this flag a Chromium-hosted window renders blank.
  const uint PW_RENDERFULLCONTENT = 0x00000002;

  // Captures the CLIENT area only, returning a bitmap whose (0,0) is the page's own
  // origin.
  //
  // The capture goes through PrintWindow rather than CopyFromScreen, and that is the
  // difference between a reliable gate and a flaky one. CopyFromScreen reads the
  // SCREEN, so it returns whatever window happens to be on top - the behaviour
  // harness leaves a browser open, a terminal may be raised at any moment, and the
  // measurements then describe that other window while looking perfectly plausible.
  // PrintWindow asks THIS window to render itself, so what is measured is the
  // program under test regardless of z-order.
  // PrintWindow renders the WHOLE window - frame and title bar included - into the
  // DC at (0,0). So the bitmap is the window's size, and the page's own origin
  // inside it is the client-to-window offset the caller asks for separately. Getting
  // this wrong is not cosmetic: measuring the page as if it started at (0,0) shifts
  // every band down by the title bar's height, which merges the header row into the
  // title bar and makes the detail-pane strip land on the wrong pixels.
  public static Bitmap ShotWindow(IntPtr hwnd, out int w, out int h, out int originX, out int originY) {
    RECT wr; GetWindowRect(hwnd, out wr);
    w = wr.Right - wr.Left; h = wr.Bottom - wr.Top;
    originX = 0; originY = 0;
    if (w <= 0 || h <= 0) return null;
    POINT o; o.X = 0; o.Y = 0;
    ClientToScreen(hwnd, ref o);
    originX = o.X - wr.Left;
    originY = o.Y - wr.Top;
    var bmp = new Bitmap(w, h, PixelFormat.Format32bppArgb);
    using (var g = Graphics.FromImage(bmp)) {
      IntPtr dc = g.GetHdc();
      try { PrintWindow(hwnd, dc, PW_RENDERFULLCONTENT); }
      finally { g.ReleaseHdc(dc); }
    }
    return bmp;
  }

  // Is this bitmap plausibly a dark-themed page rather than a screenshot of
  // something else? Used to reject a capture taken while another window covered the
  // market: the theme's background is a very dark grey (#181a20 / #1f222a), so a
  // capture dominated by bright pixels is not this page.
  public static double DarkFraction(Bitmap bmp) {
    int stride, W, H;
    byte[] buf = BufStatic(bmp, out stride, out W, out H);
    long dark = 0, tot = 0;
    for (int y = 0; y < H; y += 2) {
      int row = y*stride;
      for (int x = 0; x < W; x += 2) {
        int i = row + x*4;
        tot++;
        int lum = (buf[i] + buf[i+1] + buf[i+2]) / 3;
        if (lum < 90) dark++;
      }
    }
    return tot > 0 ? (double)dark / tot : 0.0;
  }

  static byte[] BufStatic(Bitmap bmp, out int stride, out int W, out int H) {
    W = bmp.Width; H = bmp.Height;
    var bd = bmp.LockBits(new Rectangle(0,0,W,H), ImageLockMode.ReadOnly, PixelFormat.Format32bppArgb);
    stride = bd.Stride;
    byte[] buf = new byte[bd.Stride*H];
    Marshal.Copy(bd.Scan0, buf, 0, buf.Length);
    bmp.UnlockBits(bd);
    return buf;
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

  // The band's dominant colour, as a coarse 3-bit-per-channel bucket. A histogram
  // is used because a header row's background is by definition the one shade
  // covering most of it, and deriving it from the pixels keeps the measurement
  // honest if the theme changes.
  // The band's dominant colour, as the MEAN colour of the histogram's winning
  // bucket.
  //
  // The mean matters, and the first version of this got it wrong in a way that made
  // every measurement meaningless: it returned the bucket's FLOOR (the coarse
  // colour, e.g. (0,32,32) for a real pixel of (31,34,42)). Comparing real pixels
  // against that floor makes the background itself differ by ~43, so at any
  // tolerance below that EVERY pixel counts as ink - the header collapsed into one
  // run spanning the page, and the detail strip read 100% "glyph pixels" while
  // showing nothing but background.
  //
  // The histogram still picks which colour is dominant - that is robust to text and
  // borders, which is why it is used instead of a single sample - but the colour
  // reported back is the average of the pixels in that bucket, so "differs from the
  // background" means what it says.
  static void Dominant(byte[] buf, int stride, int W, int H, int x0, int y0, int w, int h,
                       out int br, out int bg, out int bb) {
    int[] hist = new int[512];
    long[] sr = new long[512];
    long[] sg = new long[512];
    long[] sb = new long[512];
    for (int y = y0; y < y0+h && y < H; y++) {
      if (y < 0) continue;
      int row = y*stride;
      for (int x = x0; x < x0+w && x < W; x++) {
        if (x < 0) continue;
        int i = row + x*4;
        int k = ((buf[i+2]>>5)<<6) | ((buf[i+1]>>5)<<3) | (buf[i]>>5);
        hist[k]++;
        sr[k] += buf[i+2]; sg[k] += buf[i+1]; sb[k] += buf[i];
      }
    }
    int best = 0;
    for (int k = 1; k < 512; k++) if (hist[k] > hist[best]) best = k;
    if (hist[best] == 0) { br = 0; bg = 0; bb = 0; return; }
    br = (int)(sr[best] / hist[best]);
    bg = (int)(sg[best] / hist[best]);
    bb = (int)(sb[best] / hist[best]);
  }

  static bool Differs(byte[] buf, int i, int br, int bg, int bb, int tol) {
    int b = buf[i], g = buf[i+1], r = buf[i+2];
    int d = Math.Abs(r-br) + Math.Abs(g-bg) + Math.Abs(b-bb);
    return d > tol;
  }

  // Runs of "ink" columns in a band: a column is ink when any of its pixels differs
  // from the band's dominant colour. Returns "start,end;" pairs.
  //
  // Gaps of at least `minGap` blank columns separate runs. The header's controls are
  // laid out with an 8px gap, so 5 is comfortably below the real spacing while
  // still splitting them - which is what lets this find the input and the two
  // buttons as three separate boxes.
  public static string Runs(Bitmap bmp, int y0, int h, int x0, int w, int tol, int minGap) {
    int stride, W, H;
    byte[] buf = Buf(bmp, out stride, out W, out H);
    int br, bg, bb;
    Dominant(buf, stride, W, H, x0, y0, w, h, out br, out bg, out bb);
    return RunsAgainst(buf, stride, W, H, y0, h, x0, w, br, bg, bb, tol, minGap);
  }

  // Runs, judged against a background the caller supplies. This is the one the
  // header check uses; see RunsAgainst for why the estimate above is not good
  // enough there.
  public static string RunsWithBg(Bitmap bmp, int y0, int h, int x0, int w,
                                  int br, int bg, int bb, int tol, int minGap) {
    int stride, W, H;
    byte[] buf = Buf(bmp, out stride, out W, out H);
    return RunsAgainst(buf, stride, W, H, y0, h, x0, w, br, bg, bb, tol, minGap);
  }

  // The colour of one pixel, as [r,g,b]. Used to read the header's true background
  // from a point the layout cannot reach, instead of estimating it from a band the
  // address input dominates (see RunsAgainst).
  public static int[] Pixel(Bitmap bmp, int x, int y) {
    int stride, W, H;
    byte[] buf = Buf(bmp, out stride, out W, out H);
    if (x < 0 || y < 0 || x >= W || y >= H) return new int[] { -1, -1, -1 };
    int i = y*stride + x*4;
    return new int[] { buf[i+2], buf[i+1], buf[i] };
  }

  // The same measurement against a background colour the CALLER supplies.
  //
  // This overload exists because estimating the background from the band itself
  // breaks down exactly where the header check needs it most. `Dominant` returns the
  // band's most common colour; once the address input is wide - which is the state
  // the check is supposed to confirm - the input's own fill (#14161b) covers more of
  // the band than the header's (#1f222a), so the estimate becomes the INPUT's
  // colour and every column of the header that is NOT the input (including the
  // input's own 1px border, and both buttons) counts as ink. The columns then merge
  // across the 8px gaps into ONE run, the run count came back as 1 instead of 3, and
  // the script fell into its "could not be measured in this environment" branch and
  // skipped the check - reporting a note, not a failure, so the property went
  // unverified while the gate stayed green.
  //
  // Sampling an empty pixel instead is exact: the caller passes the header's real
  // background, read from a corner the layout cannot reach.
  public static string RunsAgainst(byte[] buf, int stride, int W, int H,
                                   int y0, int h, int x0, int w,
                                   int br, int bg, int bb, int tol, int minGap) {
    var cols = new List<int>();
    for (int x = x0; x < x0+w && x < W; x++) {
      if (x < 0) continue;
      bool ink = false;
      for (int y = y0; y < y0+h && y < H; y++) {
        if (y < 0) continue;
        if (Differs(buf, y*stride + x*4, br, bg, bb, tol)) { ink = true; break; }
      }
      if (ink) cols.Add(x);
    }
    if (cols.Count == 0) return "";
    var sb = new StringBuilder();
    int start = cols[0], prev = cols[0];
    for (int k = 1; k < cols.Count; k++) {
      if (cols[k] - prev >= minGap) { sb.Append(start + "," + prev + ";"); start = cols[k]; }
      prev = cols[k];
    }
    sb.Append(start + "," + prev + ";");
    return sb.ToString();
  }

  // The vertical extent of ink within one column range, as "first,last" - used to
  // prove two controls share a horizontal band (i.e. nothing wrapped to a new line).
  public static string InkRows(Bitmap bmp, int x0, int w, int y0, int h, int tol) {
    int stride, W, H;
    byte[] buf = Buf(bmp, out stride, out W, out H);
    int br, bg, bb;
    Dominant(buf, stride, W, H, x0, y0, w, h, out br, out bg, out bb);
    int first = -1, last = -1;
    for (int y = y0; y < y0+h && y < H; y++) {
      if (y < 0) continue;
      bool ink = false;
      for (int x = x0; x < x0+w && x < W; x++) {
        if (x < 0) continue;
        if (Differs(buf, y*stride + x*4, br, bg, bb, tol)) { ink = true; break; }
      }
      if (ink) { if (first < 0) first = y; last = y; }
    }
    return first < 0 ? "" : (first + "," + last);
  }

  // The share of a vertical strip that is NOT the strip's own dominant colour.
  //
  // This is the "is there text here" question, and it is asked as a FRACTION rather
  // than as a flatness test on purpose: a strip crossing the pane also crosses the
  // 1px borders of the permission cards, and a strict "every pixel identical" test
  // would fail on those borders while telling us nothing. Glyph pixels occupy many
  // rows, so a small fraction threshold separates "a border line" (well under 1%)
  // from "text runs into this strip".
  public static double DeviationFraction(Bitmap bmp, int x, int y0, int h, int tol) {
    int stride, W, H;
    byte[] buf = Buf(bmp, out stride, out W, out H);
    if (x < 0 || x >= W) return 1.0;
    int br, bg, bb;
    Dominant(buf, stride, W, H, x, y0, 1, h, out br, out bg, out bb);
    long off = 0, tot = 0;
    for (int y = y0; y < y0+h && y < H; y++) {
      if (y < 0) continue;
      tot++;
      if (Differs(buf, y*stride + x*4, br, bg, bb, tol)) off++;
    }
    return tot > 0 ? (double)off / tot : 1.0;
  }

  // How much ink a region holds, as a fraction - used to prove a measurement
  // actually saw content rather than an empty pane.
  public static double InkFraction(Bitmap bmp, int x0, int y0, int w, int h, int tol) {
    int stride, W, H;
    byte[] buf = Buf(bmp, out stride, out W, out H);
    int br, bg, bb;
    Dominant(buf, stride, W, H, x0, y0, w, h, out br, out bg, out bb);
    long ink = 0, tot = 0;
    for (int y = y0; y < y0+h && y < H; y++) {
      if (y < 0) continue;
      int row = y*stride;
      for (int x = x0; x < x0+w && x < W; x++) {
        if (x < 0) continue;
        tot++;
        if (Differs(buf, row + x*4, br, bg, bb, tol)) ink++;
      }
    }
    return tot > 0 ? (double)ink / tot : 0.0;
  }
}
'@

# --- start the market -------------------------------------------------------

$env:PETPC_DATA_DIR = $data
$proc = Start-Process -FilePath $exe -PassThru
[MK]::Pid = [uint32]$proc.Id
$hwnd = [IntPtr]::Zero
for ($i=0; $i -lt 100; $i++) {
    Start-Sleep -Milliseconds 50
    if ($proc.HasExited) {
        "NO INTERACTIVE DESKTOP: the market exited during startup ($($proc.ExitCode))."
        "This environment cannot run a windowed check; the same convention as"
        "verify-region.ps1 and verify-live.ps1 applies, and it is NOT a code failure."
        ""
        "NO INTERACTIVE DESKTOP"
        exit 0
    }
    $hwnd = [MK]::Find([uint32]$proc.Id)
    if ($hwnd -ne [IntPtr]::Zero) { break }
}
if ($hwnd -eq [IntPtr]::Zero) {
    if (-not $proc.HasExited) { $proc.Kill() }
    "NO INTERACTIVE DESKTOP: the market's window was never created."
    "This environment cannot run a windowed check; the same convention as"
    "verify-region.ps1 and verify-live.ps1 applies, and it is NOT a code failure."
    ""
    "NO INTERACTIVE DESKTOP"
    exit 0
}
Start-Sleep -Seconds 4

# --- raise the window and take a capture that is really this page ------------
#
# `CopyFromScreen` reads the screen, so the market must be in FRONT. It is raised
# here and the result is verified rather than assumed: if another window stays on
# top, the capture would be of that window and every measurement below would be
# about something else entirely - which is exactly what happened on the first run of
# this script, where the behaviour harness's browser was still foreground and the
# "detail pane" measured 99.99% ink because it was a browser's pixels.
#
# The capture is also sanity-checked against the theme before it is used. The market
# paints a very dark background (#181a20) with light text on it, so a capture that is
# mostly BRIGHT pixels is not this page; that check catches the same class of mistake
# even when GetForegroundWindow happens to agree.
[MK]::Raise($hwnd)

$wr = New-Object MK+RECT
[void][MK]::GetWindowRect($hwnd, [ref]$wr)
$ww = $wr.Right - $wr.Left
$wh = $wr.Bottom - $wr.Top
if ($ww -le 0 -or $wh -le 0) {
    if (-not $proc.HasExited) { $proc.Kill() }
    "NO INTERACTIVE DESKTOP: the market's window has no area (${ww}x${wh})."
    "This environment cannot run a windowed check; the same convention as"
    "verify-region.ps1 and verify-live.ps1 applies, and it is NOT a code failure."
    ""
    "NO INTERACTIVE DESKTOP"
    exit 0
}

$cw = 0
$ch = 0
$ox = 0
$oy = 0
$bmp = [MK]::ShotWindow($hwnd, [ref]$cw, [ref]$ch, [ref]$ox, [ref]$oy)
if ($null -eq $bmp -or $cw -le 0 -or $ch -le 0) {
    if (-not $proc.HasExited) { $proc.Kill() }
    "NO INTERACTIVE DESKTOP: the market's client area has no area."
    ""
    "NO INTERACTIVE DESKTOP"
    exit 0
}
# The page's origin inside the captured bitmap. Every band below is expressed in
# PAGE coordinates and shifted by these, so the frame and title bar - which
# PrintWindow necessarily includes - cannot land inside a measured band.
$clientRect = New-Object MK+RECT
[void][MK]::GetClientRect($hwnd, [ref]$clientRect)
$cw = $clientRect.Right - $clientRect.Left
$ch = $clientRect.Bottom - $clientRect.Top
"  page origin inside the capture = ($ox,$oy); page size = ${cw}x${ch}"

$dark = [MK]::DarkFraction($bmp)
if ($dark -lt 0.5) {
    if (-not $proc.HasExited) { $proc.Kill() }
    "NO INTERACTIVE DESKTOP: the capture is not the market's dark page"
    "  ($([Math]::Round(100 * $dark, 1))% dark pixels; the theme is #181a20)."
    "Another window was covering it while the screenshot was taken, so no"
    "measurement here would be about this program. Not a code failure."
    ""
    "NO INTERACTIVE DESKTOP"
    exit 0
}

$fail = 0
function Check($name, $ok, $detail) {
    if ($ok) { "  PASS  $name" } else { "  FAIL  $name -- $detail"; $script:fail++ }
}

# --- T124: a resizable frame -------------------------------------------------

"=== window frame ==="
$style = [MK]::Style($hwnd)
$thick = ($style -band [MK]::WS_THICKFRAME) -ne 0
$maxim = ($style -band [MK]::WS_MAXIMIZEBOX) -ne 0
"  style = 0x$($style.ToString('x8'))  WS_THICKFRAME=$thick  WS_MAXIMIZEBOX=$maxim"

Check "the market window is resizable (WS_THICKFRAME)" $thick `
    "WS_THICKFRAME is not set, so the user cannot widen the window to read a long permission row"
Check "the market window can be maximized (WS_MAXIMIZEBOX)" $maxim `
    "WS_MAXIMIZEBOX is not set"

# --- T124: the configured size is what opened --------------------------------
#
# The number asserted is the WINDOW rectangle, and that is worth spelling out
# because the task says "客户区尺寸约为 980x700". Wails v2's Width/Height configure the
# OUTER window - measured on this machine, a build configured for 980x700 opens a
# 980x700 window whose CLIENT area is 964x661, the difference being the border and
# the 31px title bar. Those insets are the OS's chrome metrics, not something this
# program sets or can be blamed for, so asserting the client against 980x700 would
# fail every correctly-sized window on any machine whose frame differs. What the
# task is actually pinning - "the program opens at the size it was configured for,
# with room for the panes" - is exactly the window rectangle, and the client size
# is printed beside it as the diagnostic that makes the difference visible.
"=== window size ==="
"  window = ${ww}x${wh} at ($($wr.Left),$($wr.Top))"
"  client = ${cw}x${ch}  (frame insets: $($ww-$cw)x$($wh-$ch))"

Check "the market opens at the configured 980x700 window size (${ww}x${wh})" `
    ([Math]::Abs($ww - 980) -le 16 -and [Math]::Abs($wh - 700) -le 16) `
    "window is ${ww}x${wh}, expected 980x700 +/-16"
# A framed window's client is strictly smaller; if it were equal there would be no
# frame at all, which would contradict the two style bits above.
Check "the client area is inset by a real frame" `
    ($cw -lt $ww -and $ch -lt $wh) `
    "client ${cw}x${ch} is not inset from the window ${ww}x${wh}, so there is no frame"

# --- ROUND 7: the store header ----------------------------------------------
#
# THIS SECTION'S SUBJECT CHANGED, and the note is here because the old measurement is
# still worth understanding. Rounds 1-6 measured "the address input absorbs every pixel
# its two buttons leave", because the mirror URL WAS the top bar and a clipped URL was
# one of the defects that moved the market out of the pet's panel.
#
# The address is a field in #market-sheet now (a setting, not a strip across the
# window), so that measurement has no subject. What the header has to prove instead is
# that it is the one row the design says it is: the mark, the SEARCH FIELD and the three
# window controls, in the same horizontal band, with a search field wide enough to be a
# search field.
#
# Two measurement details are unchanged because they are still load-bearing:
#
#   * the BAND must exclude the header's own bottom border - that line spans the full
#     width, so including it makes every column ink and merges the row into one run;
#   * the BACKGROUND must be SAMPLED from the page's top-left corner, not estimated from
#     the band's most common colour - the search box's own fill would otherwise become
#     "the background" and the whole row would read as ink. (That bug silently skipped
#     this section for a whole round; see RunsAgainst.)
$headTop = 8
$headH = 48
$tol = 30
$bgPixel = [MK]::Pixel($bmp, $ox + 2, $oy + 2)
$bgR = $bgPixel[0]; $bgG = $bgPixel[1]; $bgB = $bgPixel[2]
$runs = [MK]::RunsWithBg($bmp, $oy + $headTop, $headH, $ox, $cw, $bgR, $bgG, $bgB, $tol, 5)
"=== store header ==="
"  header background sampled at page (2,2) = rgb($bgR,$bgG,$bgB)"
"  header content band y=$headTop..$($headTop + $headH), ink runs (x0,x1): $runs"

$segments = @()
foreach ($part in ($runs -split ';')) {
    if ($part -match '^(\d+),(\d+)$') {
        $x0 = [int]$Matches[1]; $x1 = [int]$Matches[2]
        $segments += [pscustomobject]@{ X0 = $x0; X1 = $x1; W = $x1 - $x0 + 1 }
    }
}

# Mark + search + refresh + settings + theme = five boxes. A header that failed to lay
# out - or that dropped a control - measures fewer, and that is a FAILURE, not a note:
# a check that cannot measure what it exists to measure has not passed.
Check "the header draws its controls as separate boxes (found $($segments.Count))" `
    ($segments.Count -ge 4) `
    "found $($segments.Count) boxes in the header band; expected the mark, the search field and three window controls"

if ($segments.Count -ge 4) {
    "  boxes: " + (($segments | ForEach-Object { "$($_.X0)-$($_.X1)($($_.W))" }) -join ' ')

    # The search field is the WIDEST box on the row by construction (max-width 440,
    # min-width 150). Measuring the widest box rather than the second one keeps this
    # robust to the exact number of icon buttons.
    $widest = $segments | Sort-Object -Property W -Descending | Select-Object -First 1
    Check "the search field is a real field (>= 150px wide)" ($widest.W -ge 150) `
        "the widest box in the header is $($widest.W)px; a search field that narrow is a placeholder"

    # …and the row is ONE line: the ink inside the leftmost box and inside the rightmost
    # one must share a band. A control that wrapped to a second line would not.
    $leftBox = $segments[0]
    $rightBox = $segments[$segments.Count - 1]
    $leftRows = [MK]::InkRows($bmp, $ox + $leftBox.X0, $leftBox.W, $oy + $headTop, $headH, $tol)
    $rightRows = [MK]::InkRows($bmp, $ox + $rightBox.X0, $rightBox.W, $oy + $headTop, $headH, $tol)
    "  leftmost box rows = $leftRows   rightmost box rows = $rightRows"
    $overlap = $false
    if ($leftRows -and $rightRows) {
        $l = $leftRows -split ','
        $r = $rightRows -split ','
        $overlap = ([int]$l[0] -le [int]$r[1]) -and ([int]$r[0] -le [int]$l[1])
    }
    Check "the header is one row, not two" $overlap `
        "the leftmost and rightmost header boxes do not share a horizontal band ($leftRows vs $rightRows)"
}

# --- T125: no text reaches the page's right edge -----------------------------
#
# ROUND 7 NOTE: this used to describe a two-pane window ("the detail pane is the RIGHT
# half of the work area, .pane has padding 14px 16px 24px"). The window is one page at a
# time now, but the property is the same one and it is still the reason the padding is a
# measured constant: the page's border box runs to the client's right edge and its
# content box stops 20px short of it, so text that overflowed instead of wrapping would
# sit in that band, and text clipped by the page would be cut exactly there.
#
# So the strip sampled is the 2px immediately inside the CONTENT edge, and the
# padding is written as a constant mirroring the stylesheet - the same way the other
# pixel gates reproduce the layout constants they check. It is sampled as a FRACTION
# of deviating pixels rather than as strict flatness: the strip also crosses the 1px
# borders of the permission cards, and demanding byte-identical pixels would fail on
# those borders while proving nothing. Glyphs cover many rows; a border line covers
# one or two.
$panePad = 20
$workTop = 72
$workH = $ch - $workTop - 12
$detailX0 = [int]($cw * 0.55)

# Non-vacuity first: the page must actually have drawn something. Without this, an empty
# page would report "no text reached the edge" for the trivial reason that no text was
# drawn at all.
$detailInk = [MK]::InkFraction($bmp, $ox + $detailX0, $oy + $workTop, $cw - $detailX0, $workH, $tol)
"=== plugin page ==="
"  page band x=$detailX0..$($cw-1) y=$workTop..$($workTop + $workH), ink = $([Math]::Round(100 * $detailInk, 2))%"

Check "the plugin page rendered content to measure" ($detailInk -gt 0.01) `
    "the page is blank ($([Math]::Round(100 * $detailInk, 2))% ink), so nothing could be measured"

# THE TWO-COLUMN BODY (round 7). The page is the workspace plus a 300px permission rail;
# what this pins is that the RAIL was drawn. A rail that collapsed would put the
# permission list under the workspace, which still "works" - so the only way to notice
# is to measure ink where the rail is supposed to be, below the hero and to the right of
# the padding.
$railW = 300
$railX = $cw - $panePad - 12 - $railW
$bodyTop = 276
$bodyH = $ch - $bodyTop - 20
$railInk = [MK]::InkFraction($bmp, $ox + $railX, $oy + $bodyTop, $railW, $bodyH, $tol)
"  permission rail band x=$railX..$($railX + $railW - 1) y=$bodyTop..$($bodyTop + $bodyH), ink = $([Math]::Round(100 * $railInk, 2))%"

Check "the permission rail drew its rows beside the workspace" ($railInk -gt 0.03) `
    "the rail band is nearly empty ($([Math]::Round(100 * $railInk, 2))% ink), so the permission list is not where the layout says it is"

# …and the workspace column is still there, to the LEFT of the rail. The band starts at
# the top of the body, so it covers the workspace's caption strip - which is the part
# that carries ink for a plugin whose own document is a line of text (the fixture's is).
$workInk = [MK]::InkFraction($bmp, $ox + $panePad, $oy + $bodyTop, ($railX - $panePad - 14), $bodyH, $tol)
"  workspace band x=$panePad..$($railX - 14) y=$bodyTop..$($bodyTop + $bodyH), ink = $([Math]::Round(100 * $workInk, 2))%"
Check "the workspace column drew its caption strip and document" ($workInk -gt 0.01) `
    "the workspace band is nearly empty ($([Math]::Round(100 * $workInk, 2))% ink), so the plugin's column collapsed"

# SAMPLED IN THE PADDING BAND, not 2px inside the content edge.
#
# It used to be the latter, and that worked while the page's own cards stopped a couple
# of pixels short of their column. Round 7's hero card is a full-width surface that ends
# AT the content edge, so a column just inside it reads the CARD's fill as "glyphs" -
# 18% of the strip, on a page with no overflow at all. The property being checked is "no
# text is clipped by the window", and text that overflowed would be painted in the
# padding band and nowhere else, so that band is where the sample belongs.
$stripX = $cw - [int]($panePad / 2)
$dev = [MK]::DeviationFraction($bmp, $ox + $stripX, $oy + $workTop, $workH, $tol)
"  sampling the padding band at x=$stripX (page padding ${panePad}px, so this is ${stripX}..$($cw-1) minus the content edge)"
"  deviating pixels in that strip = $([Math]::Round(100 * $dev, 2))%"

Check "no text reaches the page's right edge (2px strip is background)" `
    ($dev -le 0.05) `
    "$([Math]::Round(100 * $dev, 2))% of the strip at x=$stripX is glyph pixels, so text crosses the page's right edge"

[MK]::Lower($hwnd)
if (-not $proc.HasExited) { $proc.Kill(); $proc.WaitForExit(5000) | Out-Null }

""
if ($fail -eq 0) { "ALL PLUGIN MARKET CHECKS PASSED" } else { "$fail CHECK(S) FAILED"; exit 1 }

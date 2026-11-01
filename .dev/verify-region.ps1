# Verifies the region-clipping design.
#
# Under this design the WINDOW never changes size or position when the panel is
# toggled; only a native region crop changes. So the invariants to check are:
#   * window rect identical in both states
#   * the PET's screen position identical in both states
#   * the panel is actually visible when open
#   * dragging moves the pet
#   * the panel is cropped away (clicks outside the pet reach the desktop)

$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'env.ps1')

# The repo root is DERIVED, not written down: these scripts used to hardcode
# `F:\pet_pc\...`, and when the workspace moved the three pet pixel gates died at
# Start-Process with "cannot find the file specified" - failing, not skipping, so the
# summary blamed the build. A gate that cannot find its own binary must not be able to
# report a verdict about it.
$repoRoot = Split-Path -Parent $PSScriptRoot
$exe  = if ($args.Count -ge 1) { $args[0] } else { Join-Path $repoRoot 'build\bin\PetPC.exe' }
$data = Join-Path $PSScriptRoot 'verify-region'
Remove-Item -Recurse -Force $data -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Force -Path $data | Out-Null

$now = Get-Date
$fmt = { param($d) $d.ToString('yyyy-MM-ddTHH:mm:sszzz') }
$json = @"
[
 {"id":"1","title":"Overdue task","due":"$(& $fmt $now.AddHours(-5))","done":false,"createdAt":"$(& $fmt $now)"},
 {"id":"2","title":"First task","due":"$(& $fmt $now.AddHours(3))","done":false,"createdAt":"$(& $fmt $now)"},
 {"id":"3","title":"Second task","done":false,"createdAt":"$(& $fmt $now)"}
]
"@
$utf8 = New-Object System.Text.UTF8Encoding($false)
[System.IO.File]::WriteAllText("$data\todos.json", $json, $utf8)
[System.IO.File]::WriteAllText("$data\config.json", '{"petScale":1,"snapToEdge":true,"edgeMargin":8,"autostart":false}', $utf8)

Add-Type -AssemblyName System.Drawing
Add-Type -ReferencedAssemblies 'System.Drawing' -TypeDefinition @'
using System;
using System.Drawing;
using System.Drawing.Imaging;
using System.Runtime.InteropServices;
using System.Text;

public class VR {
  [DllImport("user32.dll")] public static extern bool EnumWindows(EnumProc cb, IntPtr p);
  public delegate bool EnumProc(IntPtr h, IntPtr p);
  [DllImport("user32.dll")] public static extern uint GetWindowThreadProcessId(IntPtr h, out uint pid);
  [DllImport("user32.dll", CharSet=CharSet.Unicode)] public static extern int GetClassName(IntPtr h, StringBuilder s, int n);
  [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr h, out RECT r);
  [DllImport("user32.dll")] public static extern bool SetCursorPos(int x, int y);
  [DllImport("user32.dll")] public static extern void mouse_event(uint f, uint dx, uint dy, uint d, IntPtr e);
  [StructLayout(LayoutKind.Sequential)] public struct RECT { public int Left, Top, Right, Bottom; }
  public const uint LEFTDOWN=0x0002, LEFTUP=0x0004;

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
  public static void Click(int x,int y){ SetCursorPos(x,y); System.Threading.Thread.Sleep(140);
    mouse_event(LEFTDOWN,0,0,0,IntPtr.Zero); System.Threading.Thread.Sleep(70); mouse_event(LEFTUP,0,0,0,IntPtr.Zero); }
  public static void Down(){ mouse_event(LEFTDOWN,0,0,0,IntPtr.Zero); }
  public static void Up(){ mouse_event(LEFTUP,0,0,0,IntPtr.Zero); }

  // The cat is a large warm-coloured blob. Columns/rows are counted so that thin
  // orange text in the panel cannot be mistaken for the pet.
  public static void CatBox(out int l, out int t, out int w, out int h) {
    RECT r; IntPtr hwnd = Find(Pid);
    GetWindowRect(hwnd, out r);
    l = t = w = h = -1;
    int W = r.Right-r.Left, H = r.Bottom-r.Top;
    if (W <= 0 || H <= 0) return;
    var cols = new int[W];
    var rows = new int[H];
    var bmp = new Bitmap(W, H, PixelFormat.Format32bppArgb);
    using (var g = Graphics.FromImage(bmp))
      g.CopyFromScreen(r.Left, r.Top, 0, 0, new Size(W, H), CopyPixelOperation.SourceCopy);
    var bd = bmp.LockBits(new Rectangle(0,0,W,H), ImageLockMode.ReadOnly, PixelFormat.Format32bppArgb);
    byte[] buf = new byte[bd.Stride*H];
    Marshal.Copy(bd.Scan0, buf, 0, buf.Length);
    bmp.UnlockBits(bd);
    bmp.Dispose();
    // Scan ONLY the pet's column. The window is a constant size and the pet's
    // offset inside it never changes, so the cat must appear at a fixed place
    // there. Scanning the whole window also picks up the DESKTOP showing through
    // the cropped-away area, and a warm wallpaper then merges with the cat into a
    // single blob that spans the window - which looked like the pet moving when
    // it had not.
    int scanFrom = W - 170;
    for (int y=0; y<H; y++) {
      int row = y*bd.Stride;
      for (int x=scanFrom; x<W; x++) {
        int i = row + x*4;
        byte b = buf[i], gg = buf[i+1], rr = buf[i+2];
        if (rr > 195 && gg > 120 && gg < 235 && b < 190 && (rr-b) > 45) { cols[x]++; rows[y]++; }
      }
    }
    int L=-1,R=-1,s=-1;
    for (int x=0;x<W;x++){ if(cols[x]>=20){ if(s<0)s=x; } else { if(s>=0){ if(L<0)L=s; R=x-1; s=-1; } } }
    if(s>=0){ if(L<0)L=s; R=W-1; }
    int T=-1,B=-1,s2=-1;
    for (int y=0;y<H;y++){ if(rows[y]>=20){ if(s2<0)s2=y; } else { if(s2>=0){ if(T<0)T=s2; B=y-1; s2=-1; } } }
    if(s2>=0){ if(T<0)T=s2; B=H-1; }
    l=L; t=T; w=(R-L+1); h=(B-T+1);
  }

  public static uint Pid;
  // Fraction of the window occupied by the dark panel background.
  public static double PanelFraction() {
    RECT r; IntPtr hwnd = Find(Pid);
    GetWindowRect(hwnd, out r);
    int W = r.Right-r.Left, H = r.Bottom-r.Top;
    if (W<=0||H<=0) return 0;
    using (var bmp = new Bitmap(W, H, PixelFormat.Format32bppArgb)) {
      using (var g = Graphics.FromImage(bmp))
        g.CopyFromScreen(r.Left, r.Top, 0, 0, new Size(W, H), CopyPixelOperation.SourceCopy);
      var bd = bmp.LockBits(new Rectangle(0,0,W,H), ImageLockMode.ReadOnly, PixelFormat.Format32bppArgb);
      byte[] buf = new byte[bd.Stride*H];
      Marshal.Copy(bd.Scan0, buf, 0, buf.Length);
      bmp.UnlockBits(bd);
      long dark=0, tot=0;
      for (int y=0;y<H;y+=2){ int row=y*bd.Stride;
        for (int x=0;x<W;x+=2){ int i=row+x*4; byte b=buf[i], gg=buf[i+1], rr=buf[i+2]; tot++;
          if (rr>=25&&rr<=70&&gg>=25&&gg<=70&&b>=25&&b<=80) dark++; } }
      return tot>0 ? 100.0*dark/tot : 0;
    }
  }
}
'@

$env:PETPC_DATA_DIR = $data
$proc = Start-Process -FilePath $exe -PassThru
[VR]::Pid = [uint32]$proc.Id
$hwnd = [IntPtr]::Zero
for ($i=0; $i -lt 100; $i++) {
    Start-Sleep -Milliseconds 50
    if ($proc.HasExited) { throw "exited ($($proc.ExitCode))" }
    $hwnd = [VR]::Find([uint32]$proc.Id)
    if ($hwnd -ne [IntPtr]::Zero) { break }
}
if ($hwnd -eq [IntPtr]::Zero) { $proc.Kill(); throw "no window" }
Start-Sleep -Seconds 3

function Rect { $r = New-Object VR+RECT; [void][VR]::GetWindowRect($hwnd, [ref]$r); return $r }
function CatBox { $l=0;$t=0;$w=0;$h=0; [VR]::CatBox([ref]$l,[ref]$t,[ref]$w,[ref]$h); return "$l,$t ${w}x${h}" }

$fail = 0
function Check($name, $ok, $detail) {
    if ($ok) { "  PASS  $name" } else { "  FAIL  $name -- $detail"; $script:fail++ }
}

"=== closed state ==="
$r0 = Rect
$w0 = [int](($r0.Left + $r0.Right)/2); $h0 = [int](($r0.Top + $r0.Bottom)/2)
# Click the pet, which sits on the right side of the window.
$petClickX = $r0.Right - 75; $petClickY = [int](($r0.Top + $r0.Bottom)/2)
$cat0 = CatBox
$panel0 = [double]([VR]::PanelFraction())
"  window = ($($r0.Left),$($r0.Top)) $($r0.Right-$r0.Left)x$($r0.Bottom-$r0.Top)"
"  cat    = $cat0      panel = $([Math]::Round($panel0,1))%"

"=== a plain click on the pet (it must open nothing) ==="
# 这一节原来点的是"面板"。面板在阶 3 退休了，而单点在今天的设计里**什么都不该开**（要开卡得
# 右键，或者点宠物框里的对话按钮）。所以这里断言的正是这条：点一下之后，窗口不动、宠物不动，
# 那块深色面板也不该出现。区域什么时候被撑开，由 verify-live（插件浮层面）与行为 harness
# （气泡 / 右键菜单）分别用像素守。
[VR]::Click($petClickX, $petClickY)
Start-Sleep -Seconds 2
$r1 = Rect
$cat1 = CatBox
$panel1 = [double]([VR]::PanelFraction())
"  window = ($($r1.Left),$($r1.Top)) $($r1.Right-$r1.Left)x$($r1.Bottom-$r1.Top)"
"  cat    = $cat1      panel = $([Math]::Round($panel1,1))%"

Check "window size unchanged" (($r1.Right-$r1.Left) -eq ($r0.Right-$r0.Left) -and ($r1.Bottom-$r1.Top) -eq ($r0.Bottom-$r0.Top)) "size changed"
Check "window position unchanged" ($r1.Left -eq $r0.Left -and $r1.Top -eq $r0.Top) "position changed"
# The cat's measured box is compared on the edges that cannot be fudged.
#
# The whole box used to be compared with a 2px tolerance, and it was flaky. The
# cause is not the pet moving - measured repeatedly with nothing changing, its box
# is byte-identical (418,187 113x168, bottom=354, left=418) - it is the TOP edge:
# the scan counts "20 fur-coloured pixels in this row", and the pet's SVG has an
# anti-aliased top whose rows sit at 4..19, straddling that threshold. Which side
# of it a row lands on depends on the desktop showing through the transparent
# margins, so a warm wallpaper can flip it.
#
# The left edge and the bottom edge are solid fur hundreds of pixels deep and do
# not move at all, and the top/height are checked through them (top + height =
# bottom, which is asserted exactly). That is a stricter test of the thing that
# matters - the pet's real position - than a fuzzy comparison of a number that
# was never precise.
$c0 = $cat0 -split '[ ,x]+'; $c1 = $cat1 -split '[ ,x]+'
$dx = [Math]::Abs([int]$c0[0] - [int]$c1[0])
Check "pet left edge unchanged (exact)" ($dx -eq 0) "cat left edge moved by ${dx}: $cat0 -> $cat1"
# bottom = top + height - 1, so this pins the pet's vertical extent without
# depending on where the anti-aliased first row happened to register.
$bot0 = [int]$c0[1] + [int]$c0[3] - 1
$bot1 = [int]$c1[1] + [int]$c1[3] - 1
Check "pet bottom edge unchanged (exact)" ($bot0 -eq $bot1) "cat bottom edge moved: $bot0 -> $bot1"
Check "pet size unchanged (exact)" ([int]$c0[3] -eq [int]$c1[3]) "cat height changed: $($c0[3]) -> $($c1[3])"
Check "a plain click opens no card" ($panel1 -lt 5) "a card appeared ($([Math]::Round($panel1,1))%) - a single click must not open anything"

"=== click again (still nothing) ==="
[VR]::Click($petClickX, $petClickY)
Start-Sleep -Seconds 2
$r2 = Rect
$cat2 = CatBox
$panel2 = [double]([VR]::PanelFraction())
"  window = ($($r2.Left),$($r2.Top)) $($r2.Right-$r2.Left)x$($r2.Bottom-$r2.Top)"
"  cat    = $cat2      panel = $([Math]::Round($panel2,1))%"
Check "window unchanged after the second click" ($r2.Left -eq $r0.Left -and $r2.Top -eq $r0.Top) "window moved"
$c2 = $cat2 -split '[ ,x]+'
$dx2 = [Math]::Abs([int]$c0[0] - [int]$c2[0])
$bot2 = [int]$c2[1] + [int]$c2[3] - 1
Check "pet left edge unchanged after the second click (exact)" ($dx2 -eq 0) "cat left edge moved by ${dx2}: $cat0 -> $cat2"
Check "pet bottom edge unchanged after the second click (exact)" ($bot0 -eq $bot2) "cat bottom edge moved: $bot0 -> $bot2"
Check "still no card" ($panel2 -lt 5) "a card is up ($([Math]::Round($panel2,1))%)"

"=== drag the pet to the left ==="
$sx = $petClickX; $sy = $petClickY
$tx = 400; $ty = 700
[VR]::SetCursorPos($sx, $sy); Start-Sleep -Milliseconds 150
[VR]::Down()
for ($i=1; $i -le 25; $i++) {
    [VR]::SetCursorPos([int]($sx + ($tx-$sx)*$i/25), [int]($sy + ($ty-$sy)*$i/25))
    Start-Sleep -Milliseconds 15
}
[VR]::Up()
Start-Sleep -Seconds 2
$r3 = Rect
$cat3 = CatBox
"  window = ($($r3.Left),$($r3.Top)) $($r3.Right-$r3.Left)x$($r3.Bottom-$r3.Top)"
"  cat    = $cat3"
Check "drag moved the window" ($r3.Left -ne $r0.Left) "window did not move (still $($r3.Left))"
Check "snapped to the left edge" ($r3.Left -lt 200) "window left = $($r3.Left)"

"=== config on disk ==="
Get-Content (Join-Path $data 'config.json')

if (-not $proc.HasExited) { $proc.Kill(); $proc.WaitForExit(5000) | Out-Null }

""
if ($fail -eq 0) { "ALL REGION CHECKS PASSED" } else { "$fail CHECK(S) FAILED"; exit 1 }

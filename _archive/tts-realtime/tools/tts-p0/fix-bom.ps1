# Adds a UTF-8 BOM to the P0 scripts, without decoding or re-encoding their text.
#
# Why this is needed at all: on this machine `pwsh` is actually Windows
# PowerShell 5.1 (PSEdition=Desktop), which parses a .ps1 as the system ANSI code
# page (GBK here) unless the file starts with a UTF-8 BOM. These scripts are full
# of Chinese comments and strings, so without the BOM the parser fails with
# baffling errors like "The string is missing the terminator" on a line that looks
# perfectly fine.
#
# It is a pure byte operation on purpose. Reading the file as text and writing it
# back would round-trip through the same broken code page and mangle the Chinese -
# which is exactly the mistake that corrupted a test file earlier in this project.
param(
    [Parameter(Mandatory = $true)]
    [string[]]$Path
)

$bom = [byte[]](0xEF, 0xBB, 0xBF)
foreach ($p in $Path) {
    if (-not (Test-Path $p)) {
        Write-Host "skip (missing): $p"
        continue
    }
    $bytes = [System.IO.File]::ReadAllBytes($p)
    if ($bytes.Length -ge 3 -and $bytes[0] -eq 0xEF -and $bytes[1] -eq 0xBB -and $bytes[2] -eq 0xBF) {
        Write-Host "ok   (already has BOM): $p"
        continue
    }
    $out = New-Object byte[] ($bom.Length + $bytes.Length)
    [Array]::Copy($bom, 0, $out, 0, $bom.Length)
    [Array]::Copy($bytes, 0, $out, $bom.Length, $bytes.Length)
    [System.IO.File]::WriteAllBytes($p, $out)
    Write-Host "done (BOM added): $p  ($($bytes.Length) -> $($out.Length) bytes)"
}

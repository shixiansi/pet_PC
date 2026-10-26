@echo off
setlocal EnableExtensions
title Import CodeBuddy accounts into OFM channel-pack

rem ===========================================================================
rem  Launcher for import-codebuddy-accounts.ps1
rem
rem  Why a launcher is needed on this machine:
rem    * `pwsh` (PowerShell 7) is NOT on the persistent PATH - DSH injects it
rem      only inside its own sessions. Once DSH is closed, typing `pwsh` fails
rem      with "not recognized as an internal or external command".
rem    * The system `powershell` (5.1) IS available, but this script's source is
rem      UTF-8 without BOM, so 5.1 decodes the Chinese strings as ANSI and the
rem      script fails to parse with misleading "unexpected token" errors.
rem  So this launcher finds a PowerShell 7 build and runs the script with it.
rem
rem  Usage:
rem    double-click                       -> dry run (prints the plan, writes nothing)
rem    run-import-codebuddy-accounts.cmd -Apply   -> actually write the two files
rem ===========================================================================

set "SCRIPT=%~dp0import-codebuddy-accounts.ps1"
if not exist "%SCRIPT%" (
    echo [ERROR] Script not found next to this launcher:
    echo         %SCRIPT%
    goto :fail
)

rem ---- locate a PowerShell 7 interpreter ------------------------------------
set "PWSH="

rem 1) already on PATH?
for /f "delims=" %%P in ('where pwsh 2^>nul') do (
    if not defined PWSH set "PWSH=%%P"
)

rem 2) the copy bundled inside the DSH web profile (present on this machine)
if not defined PWSH call :try_bundled

rem 3) a normal per-machine install
if not defined PWSH call :try_programfiles
if not defined PWSH call :try_windowsapps

if not defined PWSH (
    echo [ERROR] PowerShell 7 ^(pwsh^) was not found.
    echo         Checked: PATH, the DSH-bundled copy, Program Files, WindowsApps.
    echo         Install it with:  winget install Microsoft.PowerShell
    goto :fail
)

echo [launcher] pwsh   : %PWSH%
echo [launcher] script : %SCRIPT%
if "%~1"=="" (
    echo [launcher] mode   : DRY RUN ^(nothing is written^)
) else (
    echo [launcher] args   : %*
)
echo.

rem Pass every argument through, so `-Apply` reaches the script.
"%PWSH%" -NoProfile -ExecutionPolicy Bypass -File "%SCRIPT%" %*
set "CODE=%ERRORLEVEL%"

echo.
if "%CODE%"=="0" (
    echo [launcher] finished OK.
) else (
    echo [launcher] exited with code %CODE%.
)
echo.
pause
exit /b %CODE%

rem ---- helpers (kept as subroutines so no delayed expansion is needed) ------

:try_bundled
set "CAND=%USERPROFILE%\.dsh\profiles\web\node_modules\@dsh-ops\pwsh-win32-x64\bin\pwsh.exe"
if exist "%CAND%" set "PWSH=%CAND%"
goto :eof

:try_programfiles
set "CAND=%ProgramFiles%\PowerShell\7\pwsh.exe"
if exist "%CAND%" set "PWSH=%CAND%"
goto :eof

:try_windowsapps
set "CAND=%LOCALAPPDATA%\Microsoft\WindowsApps\pwsh.exe"
if exist "%CAND%" set "PWSH=%CAND%"
goto :eof

:fail
echo.
pause
exit /b 1

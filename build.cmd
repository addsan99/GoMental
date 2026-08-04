@echo off
REM ---------------------------------------------------------------------------
REM Full Wails build for GoMental on this machine.
REM
REM Go, Node, and wails are not on the system PATH, so this puts the pinned
REM toolchain locations on PATH, then runs a full `wails build`.
REM
REM   go     C:\atera\gotool\go\bin\go.exe
REM   node   C:\atera\node\node-v22.23.1-win-x64
REM   wails  C:\Users\AddySanto\go\bin\wails.exe
REM
REM Output: build\bin\GoMental.exe
REM Usage:  build.cmd            (extra args pass through to `wails build`)
REM ---------------------------------------------------------------------------
setlocal

REM set "GOROOT=C:\atera\gotool\go"
REM set "GOFLAGS=-mod=mod"
REM set "PATH=C:\atera\gotool\go\bin;C:\atera\node\node-v22.23.1-win-x64;C:\Users\AddySanto\go\bin;%PATH%"

REM Build from this script's directory regardless of the caller's cwd.
pushd "%~dp0"

echo === GoMental full Wails build =============================================
go version
where wails
echo ==========================================================================

go list unsafe >NUL 2>NUL
if not "%ERRORLEVEL%"=="0" (
    echo.
    echo Build FAILED: Go standard library is not usable. Reinstall or repair Go, then retry.
    popd
    exit /b 1
)

wails build %*
set "BUILD_RC=%ERRORLEVEL%"

popd

if not "%BUILD_RC%"=="0" (
    echo.
    echo Build FAILED with exit code %BUILD_RC%.
    exit /b %BUILD_RC%
)

echo.
echo Build succeeded: build\bin\GoMental.exe
endlocal

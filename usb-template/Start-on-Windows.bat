@echo off
rem Double-click me. Windows SmartScreen may warn because the app is not signed yet:
rem choose "More info" > "Run anyway".
cd /d "%~dp0"
if /I "%PROCESSOR_ARCHITECTURE%"=="ARM64" (jarvis-host-arm64.exe %*) else (jarvis-host.exe %*)
pause

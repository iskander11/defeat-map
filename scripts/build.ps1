# Builds the release Windows exe with no attached console window.
# Run from the repository root: powershell -ExecutionPolicy Bypass -File scripts/build.ps1
$ErrorActionPreference = "Stop"

$mingwBin = "C:\Users\LKTR\AppData\Local\Microsoft\WinGet\Packages\BrechtSanders.WinLibs.POSIX.UCRT_Microsoft.Winget.Source_8wekyb3d8bbwe\mingw64\bin"
if (Test-Path $mingwBin) {
    $env:Path = "$env:Path;$mingwBin"
}
$env:CGO_ENABLED = "1"

New-Item -ItemType Directory -Force -Path "dist" | Out-Null
go build -ldflags "-H=windowsgui -s -w" -o "dist\defeatmap.exe" ".\cmd\defeatmap"

Copy-Item -Recurse -Force "assets" "dist\assets"

Write-Output "Готово: dist\defeatmap.exe"

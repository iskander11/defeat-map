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

Remove-Item -Recurse -Force "dist\assets" -ErrorAction SilentlyContinue
Copy-Item -Recurse -Force "assets" "dist\assets"

Write-Output "Built: dist\defeatmap.exe"

# Optional: also build the Windows installer if Inno Setup is installed.
$iscc = Get-ChildItem -Path "$env:LOCALAPPDATA\Programs\Inno Setup 6\ISCC.exe", "C:\Program Files*\Inno Setup 6\ISCC.exe" -ErrorAction SilentlyContinue | Select-Object -First 1
if ($iscc) {
    & $iscc.FullName "scripts\installer.iss"
    Write-Output "Built: dist-installer\DefeatMapSetup.exe"
} else {
    Write-Output "Inno Setup not found - installer not built (get it from https://jrsoftware.org/isinfo.php, then compile scripts/installer.iss)."
}

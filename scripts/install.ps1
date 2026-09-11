# Installs mef and mef-mcp on Windows.
#
# From a checkout (works today):   .\scripts\install.ps1
# From a release (once published): .\scripts\install.ps1 -FromRelease
param(
    [switch]$FromRelease,
    [string]$Version = "latest"
)
$ErrorActionPreference = "Stop"

if (-not $FromRelease) {
    if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
        throw "Go >= 1.26 is required to install from source: https://go.dev/dl/"
    }
    $root = Split-Path -Parent $PSScriptRoot
    Push-Location $root
    try { go install ./cmd/... } finally { Pop-Location }
    $bin = Join-Path (go env GOPATH) "bin"
} else {
    $arch = if ([Environment]::Is64BitOperatingSystem -and $env:PROCESSOR_ARCHITECTURE -eq "ARM64") { "arm64" } else { "amd64" }
    $api = if ($Version -eq "latest") { "https://api.github.com/repos/JOSETRA44/mef-mcp/releases/latest" } else { "https://api.github.com/repos/JOSETRA44/mef-mcp/releases/tags/v$Version" }
    $release = Invoke-RestMethod $api
    $asset = $release.assets | Where-Object { $_.name -like "*_windows_$arch.zip" } | Select-Object -First 1
    if (-not $asset) { throw "No windows/$arch asset in release $($release.tag_name)" }
    $bin = Join-Path $env:LOCALAPPDATA "Programs\mef-mcp"
    New-Item -ItemType Directory -Force $bin | Out-Null
    $zip = Join-Path $env:TEMP $asset.name
    Invoke-WebRequest $asset.browser_download_url -OutFile $zip
    Expand-Archive $zip -DestinationPath $bin -Force
    Remove-Item $zip
}

$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if (($userPath -split ";") -notcontains $bin) {
    [Environment]::SetEnvironmentVariable("Path", "$userPath;$bin", "User")
    Write-Host "Added $bin to your user PATH. Open a new terminal."
}
& (Join-Path $bin "mef.exe") --version
Write-Host "Register with Claude Code:  claude mcp add mef -- mef-mcp"

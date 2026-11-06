# path: scripts/package.ps1
# Forge CI - Multi-Platform Release Cross-Compilation and Packaging (PowerShell)
# Builds forge-api and forge-runner binaries across Linux, macOS, and Windows.
[CmdletBinding()]
param (
    [string]$Version = "v1.0.0",
    [switch]$SkipFrontend = $false
)

$ErrorActionPreference = "Stop"
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$RootDir = Split-Path -Parent $ScriptDir
$DistDir = Join-Path $RootDir "dist"

$Commit = "none"
try {
    $Commit = (git rev-parse --short HEAD 2>$null)
    if (-not $Commit) { $Commit = "none" }
} catch {
    $Commit = "none"
}

$BuildDate = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")

Write-Host "================================================================" -ForegroundColor Cyan
Write-Host " Forge CI Multi-Platform Packaging" -ForegroundColor Cyan
Write-Host " Version:    $Version" -ForegroundColor Yellow
Write-Host " Commit:     $Commit" -ForegroundColor Yellow
Write-Host " Build Date: $BuildDate" -ForegroundColor Yellow
Write-Host " Output Dir: $DistDir" -ForegroundColor Yellow
Write-Host "================================================================" -ForegroundColor Cyan

if (Test-Path $DistDir) {
    Remove-Item -Recurse -Force $DistDir
}
New-Item -ItemType Directory -Force -Path $DistDir | Out-Null

# Build React 18 production frontend if requested
$WebDir = Join-Path $RootDir "web"
if (-not $SkipFrontend -and (Test-Path $WebDir)) {
    Write-Host "==> Building React 18 production dashboard..." -ForegroundColor Green
    Push-Location $WebDir
    try {
        if (-not (Test-Path (Join-Path $WebDir "node_modules"))) {
            npm install
        }
        npm run build
        Write-Host "==> Frontend built successfully into web/dist." -ForegroundColor Green
    } catch {
        Write-Warning "Frontend build failed or npm not available. Continuing with backend build..."
    } finally {
        Pop-Location
    }
}

$Targets = @(
    @{ OS = "linux"; Arch = "amd64"; Ext = "" },
    @{ OS = "linux"; Arch = "arm64"; Ext = "" },
    @{ OS = "darwin"; Arch = "amd64"; Ext = "" },
    @{ OS = "darwin"; Arch = "arm64"; Ext = "" },
    @{ OS = "windows"; Arch = "amd64"; Ext = ".exe" }
)

$LdFlags = "-s -w -X main.Version=$Version -X main.Commit=$Commit -X main.BuildDate=$BuildDate"

foreach ($target in $Targets) {
    $goos = $target.OS
    $goarch = $target.Arch
    $ext = $target.Ext

    $targetName = "forge-ci_${Version}_${goos}_${goarch}"
    $stageDir = Join-Path $DistDir $targetName
    $binDir = Join-Path $stageDir "bin"
    New-Item -ItemType Directory -Force -Path $binDir | Out-Null

    Write-Host "==> Compiling for $goos/$goarch..." -ForegroundColor Green

    $env:CGO_ENABLED = "0"
    $env:GOOS = $goos
    $env:GOARCH = $goarch

    # Build forge-api
    $apiOut = Join-Path $binDir ("forge-api" + $ext)
    go build -trimpath -ldflags $LdFlags -o $apiOut "$RootDir/cmd/api"
    if ($LASTEXITCODE -ne 0) {
        throw "Failed to build forge-api for $goos/$goarch"
    }

    # Build forge-runner
    $runnerOut = Join-Path $binDir ("forge-runner" + $ext)
    go build -trimpath -ldflags $LdFlags -o $runnerOut "$RootDir/cmd/runner"
    if ($LASTEXITCODE -ne 0) {
        throw "Failed to build forge-runner for $goos/$goarch"
    }

    # Copy migrations and support files
    $migSource = Join-Path $RootDir "migrations"
    if (Test-Path $migSource) {
        Copy-Item -Recurse -Path $migSource -Destination (Join-Path $stageDir "migrations")
    }

    $deploySource = Join-Path $RootDir "deployments"
    if (Test-Path $deploySource) {
        Copy-Item -Recurse -Path $deploySource -Destination (Join-Path $stageDir "deployments")
    }

    $readme = Join-Path $RootDir "README.md"
    if (Test-Path $readme) {
        Copy-Item -Path $readme -Destination $stageDir
    }

    $webDist = Join-Path $RootDir "web\dist"
    if (Test-Path $webDist) {
        Copy-Item -Recurse -Path $webDist -Destination (Join-Path $stageDir "web-dist")
    }

    # Compress stage directory to zip
    $zipFile = Join-Path $DistDir "$targetName.zip"
    Write-Host "==> Packaging archive: $targetName.zip..." -ForegroundColor Gray
    Compress-Archive -Path "$stageDir\*" -DestinationPath $zipFile -Force
    Remove-Item -Recurse -Force $stageDir
}

# Reset environment variables
$env:GOOS = ""
$env:GOARCH = ""
$env:CGO_ENABLED = ""

# Generate SHA256 checksums
Write-Host "==> Computing SHA256 checksums..." -ForegroundColor Green
$checksumFile = Join-Path $DistDir "checksums.txt"
$hashLines = @()
Get-ChildItem -Path $DistDir -Filter "*.zip" | ForEach-Object {
    $hash = (Get-FileHash -Path $_.FullName -Algorithm SHA256).Hash.ToLower()
    $hashLines += "$hash  $($_.Name)"
}
$hashLines | Out-File -FilePath $checksumFile -Encoding utf8

Write-Host "================================================================" -ForegroundColor Cyan
Write-Host " Release packaging completed successfully!" -ForegroundColor Green
Get-ChildItem -Path $DistDir | Format-Table Name, Length, LastWriteTime
Write-Host "================================================================" -ForegroundColor Cyan


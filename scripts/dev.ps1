# path: scripts/dev.ps1
<#
.SYNOPSIS
    Local development helper script for Forge CI on Windows.
.DESCRIPTION
    Runs the Forge CI API server with mock adapters enabled and starts the Vite React UI.
#>

$ErrorActionPreference = "Stop"

Write-Host "==================================================" -ForegroundColor Cyan
Write-Host "   Forge CI - Developer Environment (Nairobi, KE) " -ForegroundColor Cyan
Write-Host "==================================================" -ForegroundColor Cyan

# Set environment variables for offline mock development
$env:PORT = "8080"
$env:ENV = "development"
$env:TIMEZONE = "Africa/Nairobi"
$env:USE_MOCK_INTEGRATIONS = "true"
$env:FORGE_MASTER_KEY = "0123456789abcdef0123456789abcdef"
$env:ALERT_PHONE = "+254712345678"

Write-Host ""
Write-Host "[1/3] Verifying Go compilation..." -ForegroundColor Yellow
go vet ./...
if ($LASTEXITCODE -ne 0) {
    Write-Error "Go vet failed."
    exit 1
}

Write-Host "[2/3] Starting Forge CI API Server on :8080 (Mock Integrations Enabled)..." -ForegroundColor Green
$apiProcess = Start-Process -FilePath "go" -ArgumentList "run", "./cmd/api" -PassThru -NoNewWindow

Start-Sleep -Seconds 2

# Verify health endpoint
try {
    $health = Invoke-RestMethod -Uri "http://localhost:8080/healthz" -Method Get -TimeoutSec 3
    Write-Host "       API Health: $($health.status) (service: $($health.service))" -ForegroundColor Green
} catch {
    Write-Host "       API server is compiling in the background..." -ForegroundColor Yellow
}

Write-Host ""
Write-Host "[3/3] Starting Vite Web UI on :5173..." -ForegroundColor Cyan
Write-Host "       Dashboard: http://localhost:5173" -ForegroundColor White
Write-Host "       Press Ctrl+C to terminate both servers." -ForegroundColor DarkGray
Write-Host ""

Set-Location -Path "web"
try {
    npm run dev
} finally {
    Set-Location -Path ".."
    if ($apiProcess -and -not $apiProcess.HasExited) {
        $pidNum = $apiProcess.Id
        Write-Host ""
        Write-Host "Stopping API server process (PID $pidNum)..." -ForegroundColor Yellow
        Stop-Process -Id $pidNum -Force -ErrorAction SilentlyContinue
    }
}

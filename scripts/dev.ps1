# path: scripts/dev.ps1
<#
.SYNOPSIS
    Local development helper script for Forge CI on Windows.
.DESCRIPTION
    Runs the Forge CI API server with mock adapters enabled and starts the Vite React UI.
#>

$ErrorActionPreference = "Stop"

Write-Host "==================================================" -ForegroundColor Cyan
Write-Host "   Forge CI — Developer Environment (Nairobi, KE)  " -ForegroundColor Cyan
Write-Host "==================================================" -ForegroundColor Cyan

# Set environment variables for offline mock development
$env:PORT = "8080"
$env:ENV = "development"
$env:TIMEZONE = "Africa/Nairobi"
$env:USE_MOCK_INTEGRATIONS = "true"
$env:FORGE_MASTER_KEY = "0123456789abcdef0123456789abcdef"
$env:ALERT_PHONE = "+254712345678"

Write-Host "`n[1/3] Verifying Go compilation..." -ForegroundColor Yellow
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
    $health = Invoke-RestMethod -Uri "http://localhost:8080/healthz" -Method Get
    Write-Host "       API Health: $($health.status) (service: $($health.service))" -ForegroundColor Green
} catch {
    Write-Warning "API health probe failed. It may still be compiling."
}

Write-Host "`n[3/3] Starting Vite Web UI on :5173..." -ForegroundColor Cyan
Write-Host "       Dashboard: http://localhost:5173" -ForegroundColor White
Write-Host "       Press Ctrl+C to terminate both servers.`n" -ForegroundColor DarkGray

Set-Location -Path "web"
try {
    npm run dev
} finally {
    Set-Location -Path ".."
    if ($apiProcess -and -not $apiProcess.HasExited) {
        Write-Host "`nStopping API server process (PID $($apiProcess.Id))..." -ForegroundColor Yellow
        Stop-Process -Id $apiProcess.Id -Force
    }
}


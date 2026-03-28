<#
Install Go (if missing) and run reconciliation tests for this repository.

Usage (PowerShell):
    .\scripts\install_go_and_run_tests.ps1

What it does:
- Checks if 'go' is available on PATH. If so, prints version and runs tests.
- If 'go' is missing, attempts to install via winget (preferred) or scoop (fallback).
- If neither installer is available, prints manual instructions.

Notes:
- You may need to run PowerShell as Administrator for winget or scoop installs.
#>

Set-StrictMode -Version Latest

function Write-ErrAndExit($msg) {
    Write-Host $msg -ForegroundColor Red
    exit 1
}

Write-Host "Checking for 'go' on PATH..."
if (Get-Command go -ErrorAction SilentlyContinue) {
    Write-Host "Go is already installed:" (go version)
} else {
    Write-Host "Go not found. Trying to install..."

    $winget = Get-Command winget -ErrorAction SilentlyContinue
    $scoop = Get-Command scoop -ErrorAction SilentlyContinue

    if ($winget) {
        Write-Host "Found winget. Installing Go via winget (may require elevation)..."
        winget install --id=GoLang.Go -e --source winget
        if ($LASTEXITCODE -ne 0) {
            Write-Host "winget install failed (exit $LASTEXITCODE). Will attempt scoop if available, otherwise ask for manual install or rerun as Administrator." -ForegroundColor Yellow
            if ($scoop) {
                Write-Host "Found scoop. Trying scoop install as fallback..."
                scoop install go
                if ($LASTEXITCODE -ne 0) {
                    Write-ErrAndExit "scoop install failed (exit $LASTEXITCODE). Try running PowerShell as Administrator or install Go manually from https://go.dev/dl/."
                }
            } else {
                Write-ErrAndExit "winget install failed and scoop not available. Try running PowerShell as Administrator or install Go manually from https://go.dev/dl/."
            }
        }
    } elseif ($scoop) {
        Write-Host "Found scoop. Installing Go via scoop..."
        scoop install go
        if ($LASTEXITCODE -ne 0) {
            Write-ErrAndExit "scoop install failed (exit $LASTEXITCODE). Try installing Go manually from https://go.dev/dl/."
        }
    } else {
        Write-Host "No automatic installer (winget or scoop) found."
        Write-Host "Please install Go manually from https://go.dev/dl/ (choose the Windows MSI), then re-open PowerShell and re-run this script."
        exit 1
    }

    Write-Host "Installation finished. Please open a new PowerShell window if 'go' is still not on PATH."
    Start-Sleep -Seconds 2
    if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
        Write-Host "Warning: 'go' still not found on PATH. Open a new terminal and try 'go version'."
    } else {
        Write-Host "Go installed:" (go version)
    }
}

# Run the reconciliation package tests first (fast and isolated).
$root = Split-Path -Parent $MyInvocation.MyCommand.Path
Push-Location $root
try {
    Write-Host "Running reconciliation package tests..."
    & go test ./internal/reconciliation -v
    $recExit = $LASTEXITCODE

    Write-Host "Running handler test for reconciliation..."
    & go test ./internal/handlers -run TestReconcileHandler -v
    $hdlExit = $LASTEXITCODE

    if ($recExit -ne 0 -or $hdlExit -ne 0) {
        Write-ErrAndExit "One or more tests failed. See output above for details."
    }

    Write-Host "All reconciliation tests passed." -ForegroundColor Green
} finally {
    Pop-Location
}

# Cavekit installer (Windows).
#
# Builds the cavekit.exe Go binary, installs it under
# %LOCALAPPDATA%\Programs\cavekit, adds that directory to the user PATH, and
# delegates marketplace / settings.json / Codex linking to
# `cavekit install all`.
#
# Usage (PowerShell 7+):
#   git clone https://github.com/JuliusBrussee/cavekit.git $env:USERPROFILE\.cavekit
#   & $env:USERPROFILE\.cavekit\install.ps1

[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'

$InstallDir = (Get-Item -Path (Split-Path -Path $PSCommandPath -Parent)).FullName
$BinDir = Join-Path $env:LOCALAPPDATA 'Programs\cavekit'
$BinPath = Join-Path $BinDir 'cavekit.exe'

function Write-Info($msg) { Write-Host "▸ $msg" -ForegroundColor Blue }
function Write-Ok($msg)   { Write-Host "■ $msg" -ForegroundColor Green }
function Write-Warn($msg) { Write-Host "! $msg" -ForegroundColor Yellow }
function Fail($msg)       { Write-Host "✗ $msg" -ForegroundColor Red; exit 1 }

Write-Host ""
Write-Host "  ┌──────────────────────────┐" -ForegroundColor Blue
Write-Host "  │  C A V E K I T           │" -ForegroundColor Blue
Write-Host "  └──────────────────────────┘" -ForegroundColor Blue
Write-Host "  Installer"
Write-Host ""

# ── Preflight ────────────────────────────────────────────────────────────

if (-not (Get-Command git -ErrorAction SilentlyContinue)) { Fail "git not found." }
if (-not (Get-Command go -ErrorAction SilentlyContinue))  { Fail "go not found. Install Go 1.22+ from https://go.dev/dl/." }
if (-not (Get-Command wt -ErrorAction SilentlyContinue))  { Write-Warn "wt.exe (Windows Terminal 1.18+) not found. Required for the parallel launcher." }
if (-not (Get-Command claude -ErrorAction SilentlyContinue)) { Write-Warn "claude CLI not found. Install Claude Code to use /ck:... commands." }
if (-not (Get-Command codex -ErrorAction SilentlyContinue))  { Write-Warn "codex CLI not found. Codex local plugin sync will be skipped." }

# ── Build cavekit.exe ────────────────────────────────────────────────────

Write-Info "Building cavekit.exe..."
New-Item -ItemType Directory -Force -Path $BinDir | Out-Null
Push-Location $InstallDir
try {
    & go build -o $BinPath ./cmd/cavekit
    if ($LASTEXITCODE -ne 0) { Fail "go build failed" }
} finally {
    Pop-Location
}
Write-Ok "Built $BinPath"

# ── Ensure on PATH (user scope) ──────────────────────────────────────────

$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if (-not $userPath) { $userPath = '' }
$pathEntries = $userPath -split ';' | Where-Object { $_ -ne '' }
if ($pathEntries -notcontains $BinDir) {
    $newPath = ($pathEntries + $BinDir) -join ';'
    [Environment]::SetEnvironmentVariable('Path', $newPath, 'User')
    $env:Path = $env:Path + ';' + $BinDir
    Write-Ok "Added $BinDir to user PATH (restart shells to pick it up)"
} else {
    Write-Ok "$BinDir already on user PATH"
}

# ── Delegate to cavekit install all ──────────────────────────────────────

Write-Info "Running cavekit install all..."
& $BinPath install all
if ($LASTEXITCODE -ne 0) { Fail "cavekit install all failed" }

Write-Host ""
Write-Host "Installed!" -ForegroundColor Green
Write-Host ""
Write-Host "  Restart Claude Code and Codex to load the plugin changes."
Write-Host ""

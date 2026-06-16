#Requires -Version 7.0

<#
.DESCRIPTION
    Automated Windows binary builder for BizantiAgent release.

    This script:
    1. Builds the Windows binary
    2. Updates version file
    3. Creates git commits and tags
    4. Uploads to GitHub Releases

.EXAMPLE
    .\scripts\build-release.ps1
#>

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$RepoRoot = Split-Path -Parent $PSScriptRoot
Set-Location $RepoRoot

Write-Host "=== BizantiAgent Automated Release Builder ===" -ForegroundColor Cyan
Write-Host ""

# 1. Read current version
Write-Host "[1/6] Reading current version..." -ForegroundColor Yellow
$VersionFile = Join-Path $RepoRoot "internal/version/version.go"
$VersionMatch = Select-String -Path $VersionFile -Pattern 'Version = "([^"]+)"'
$CurrentVersion = $VersionMatch.Matches[0].Groups[1].Value

Write-Host "  Current version: $CurrentVersion"

# Parse version components
$VersionParts = $CurrentVersion.Split('.')
$Major = [int]$VersionParts[0]
$Minor = [int]$VersionParts[1]
$Patch = [int]$VersionParts[2]

# Increment patch version
$NewPatch = $Patch + 1
$NewVersion = "$Major.$Minor.$NewPatch"
$NewTag = "v$NewVersion"

Write-Host "  New version: $NewVersion" -ForegroundColor Green
Write-Host ""

# 2. Build Windows binary
Write-Host "[2/6] Building Windows binary..." -ForegroundColor Yellow
try {
    & go mod tidy
    New-Item -ItemType Directory -Force -Path "$RepoRoot\dist" | Out-Null

    & go build `
        -ldflags "-H=windowsgui -s -w" `
        -o "$RepoRoot\dist\BizantiAgent.exe" `
        ./cmd/bizanti-agent

    if (-not (Test-Path "$RepoRoot\dist\BizantiAgent.exe")) {
        throw "Binary build failed"
    }

    $BinarySize = (Get-Item "$RepoRoot\dist\BizantiAgent.exe").Length / 1MB
    Write-Host "  ✓ Binary built: BizantiAgent.exe ($([Math]::Round($BinarySize, 1)) MB)" -ForegroundColor Green
}
catch {
    Write-Host "  ✗ Build failed: $_" -ForegroundColor Red
    exit 1
}

Write-Host ""

# 3. Update version file
Write-Host "[3/6] Updating version file..." -ForegroundColor Yellow
try {
    $Content = Get-Content $VersionFile
    $Content = $Content -replace "Version = `"$CurrentVersion`"", "Version = `"$NewVersion`""
    Set-Content -Path $VersionFile -Value $Content

    & git add $VersionFile
    & git commit -m @"
Bump version to $NewVersion

Co-Authored-By: Claude Haiku 4.5 <noreply@anthropic.com>
"@

    Write-Host "  ✓ Version bumped" -ForegroundColor Green
}
catch {
    Write-Host "  ✗ Version update failed: $_" -ForegroundColor Red
    exit 1
}

Write-Host ""

# 4. Copy binary to releases folder
Write-Host "[4/6] Organizing release artifacts..." -ForegroundColor Yellow
try {
    $ReleaseDir = Join-Path $RepoRoot "releases/bizanti-agent-$NewTag-win64"
    New-Item -ItemType Directory -Force -Path $ReleaseDir | Out-Null

    Copy-Item "$RepoRoot\dist\BizantiAgent.exe" `
        -Destination "$ReleaseDir\BizantiAgent.exe" `
        -Force

    Write-Host "  ✓ Binary copied to releases/" -ForegroundColor Green
}
catch {
    Write-Host "  ✗ Copy failed: $_" -ForegroundColor Red
    exit 1
}

Write-Host ""

# 5. Create git tag and push
Write-Host "[5/6] Creating git tag and pushing..." -ForegroundColor Yellow
try {
    & git tag -a $NewTag -m "Release $NewTag - BizantiAgent"
    & git push origin main
    & git push origin $NewTag

    Write-Host "  ✓ Tag created and pushed: $NewTag" -ForegroundColor Green
}
catch {
    Write-Host "  ✗ Git operations failed: $_" -ForegroundColor Red
    exit 1
}

Write-Host ""

# 6. Create GitHub Release and upload binary
Write-Host "[6/6] Creating GitHub Release..." -ForegroundColor Yellow

# Extract GitHub token
$GitCredentials = Get-Content "$env:USERPROFILE\.git-credentials" -ErrorAction SilentlyContinue
if (-not $GitCredentials) {
    Write-Host "  ✗ GitHub token not found in ~/.git-credentials" -ForegroundColor Red
    Write-Host "     Tag created locally but release upload skipped" -ForegroundColor Yellow
    exit 0
}

$Token = ($GitCredentials -match 'https://[^:]*:([^@]*)@github' | Out-Null) ? $matches[1] : $null
if (-not $Token) {
    Write-Host "  ✗ Could not extract GitHub token" -ForegroundColor Red
    Write-Host "     Tag created locally but release upload skipped" -ForegroundColor Yellow
    exit 0
}

try {
    # Create release
    $ReleaseBody = @"
## Changes

See commit log: https://github.com/NowakAdmin/BizantiAgent/compare/v$CurrentVersion...$NewTag

Built on $(Get-Date -Format 'yyyy-MM-dd HH:mm:ss') UTC
"@

    $ReleaseParams = @{
        tag_name   = $NewTag
        name       = "Release $NewTag"
        body       = $ReleaseBody
        draft      = $false
        prerelease = $false
    } | ConvertTo-Json

    $ReleaseResponse = Invoke-WebRequest `
        -Uri "https://api.github.com/repos/NowakAdmin/BizantiAgent/releases" `
        -Method Post `
        -Headers @{ Authorization = "token $Token"; Accept = "application/vnd.github+json" } `
        -Body $ReleaseParams `
        -ContentType "application/json"

    $Release = $ReleaseResponse.Content | ConvertFrom-Json
    $ReleaseId = $Release.id

    Write-Host "  ✓ Release created (ID: $ReleaseId)" -ForegroundColor Green

    # Upload binary
    Write-Host "    Uploading binary..." -ForegroundColor Gray

    $BinaryPath = "$RepoRoot\dist\BizantiAgent.exe"
    $BinaryContent = [System.IO.File]::ReadAllBytes($BinaryPath)

    $UploadResponse = Invoke-WebRequest `
        -Uri "https://uploads.github.com/repos/NowakAdmin/BizantiAgent/releases/$ReleaseId/assets?name=BizantiAgent-$NewTag.exe" `
        -Method Post `
        -Headers @{ Authorization = "token $Token"; Accept = "application/vnd.github+json" } `
        -Body $BinaryContent `
        -ContentType "application/octet-stream"

    $Asset = $UploadResponse.Content | ConvertFrom-Json

    Write-Host "    ✓ Binary uploaded: $($Asset.name)" -ForegroundColor Green
    Write-Host "      Size: $([Math]::Round($Asset.size / 1MB, 1)) MB" -ForegroundColor Green
    Write-Host "      Download: $($Asset.browser_download_url)" -ForegroundColor Green
}
catch {
    Write-Host "  ✗ GitHub release failed: $_" -ForegroundColor Red
    Write-Host "     Tag created locally but release upload failed" -ForegroundColor Yellow
    exit 1
}

Write-Host ""
Write-Host "=== Release Complete ===" -ForegroundColor Green
Write-Host "Release:  $NewTag" -ForegroundColor Cyan
Write-Host "Binary:   https://github.com/NowakAdmin/BizantiAgent/releases/download/$NewTag/BizantiAgent-$NewVersion.exe" -ForegroundColor Cyan
Write-Host ""

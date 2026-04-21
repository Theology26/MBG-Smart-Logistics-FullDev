# ============================================================================
# MBG Smart Logistics — OSRM Data Preparation Script
# ============================================================================
# This script downloads and processes OpenStreetMap data for East Java
# (which includes Kota Malang) for use with the OSRM routing engine.
#
# Prerequisites:
#   - Podman Desktop installed and running
#   - ~2GB free disk space for map data
#
# Usage:
#   .\scripts\setup_osrm.ps1
# ============================================================================

$ErrorActionPreference = "Stop"
$ProjectRoot = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
$OSRMDataDir = Join-Path $ProjectRoot "osrm-data"

Write-Host "============================================================" -ForegroundColor Cyan
Write-Host "  MBG Smart Logistics - OSRM Setup for East Java / Malang" -ForegroundColor Cyan
Write-Host "============================================================" -ForegroundColor Cyan
Write-Host ""

# Ensure absolute paths for Podman
$OSRMDataDir = Resolve-Path $OSRMDataDir -ErrorAction SilentlyContinue
if ($null -eq $OSRMDataDir) {
    $OSRMDataDir = Join-Path $ProjectRoot "osrm-data"
}

# Step 1: Create data directory
if (-not (Test-Path $OSRMDataDir)) {
    Write-Host "[1/5] Creating OSRM data directory..." -ForegroundColor Yellow
    New-Item -ItemType Directory -Path $OSRMDataDir | Out-Null
}
else {
    Write-Host "[1/5] OSRM data directory exists." -ForegroundColor Green
}

# Step 2: Download East Java OSM data from Geofabrik
$OSMFile = Join-Path $OSRMDataDir "east-java-latest.osm.pbf"
if (-not (Test-Path $OSMFile)) {
    Write-Host "[2/5] Downloading East Java OSM data from Geofabrik..." -ForegroundColor Yellow
    Write-Host "       This may take a few minutes..." -ForegroundColor Gray

    $DownloadURL = "https://download.geofabrik.de/asia/indonesia/java-latest.osm.pbf"
    # Note: Geofabrik may not have a separate East Java extract.
    # Using full Java island data which includes Malang.
    # For a more precise extract, use Overpass API or osmium-tool.

    try {
        Invoke-WebRequest -Uri $DownloadURL -OutFile $OSMFile -UseBasicParsing
        Write-Host "       Download complete!" -ForegroundColor Green
    }
    catch {
        Write-Host "       Failed to download from Geofabrik." -ForegroundColor Red
        Write-Host "       Alternative: Download manually from https://download.geofabrik.de/asia/indonesia.html" -ForegroundColor Yellow
        Write-Host "       Place the .osm.pbf file in: $OSRMDataDir" -ForegroundColor Yellow
        exit 1
    }
}
else {
    Write-Host "[2/5] OSM data file already exists." -ForegroundColor Green
}

# Step 3: OSRM Extract — Parse the OSM data
$OSRMExtract = Join-Path $OSRMDataDir "east-java-latest.osrm"
if (-not (Test-Path $OSRMExtract)) {
    Write-Host "[3/5] Running OSRM extract (parsing OSM data)..." -ForegroundColor Yellow
    Write-Host "       This step takes 5-15 minutes depending on hardware..." -ForegroundColor Gray

    # Format path for Podman (Windows to WSL format, e.g., C:\path -> /mnt/c/path)
    $DriveLetter = $OSRMDataDir.ToString().Substring(0, 1).ToLower()
    $RelativePath = $OSRMDataDir.ToString().Substring(2).Replace('\', '/')
    $PodmanDataDir = "/mnt/$DriveLetter$RelativePath"
    
    podman run --rm -v "${PodmanDataDir}:/data" osrm/osrm-backend:latest `
        osrm-extract -p /opt/car.lua /data/east-java-latest.osm.pbf

    if ($LASTEXITCODE -ne 0) {
        Write-Host "       OSRM extract failed!" -ForegroundColor Red
        exit 1
    }
    Write-Host "       Extract complete!" -ForegroundColor Green
}
else {
    Write-Host "[3/5] OSRM extract data already exists." -ForegroundColor Green
}

# Step 4: OSRM Partition — Partition the graph for MLD algorithm
$OSRMPartition = Join-Path $OSRMDataDir "east-java-latest.osrm.partition"
if (-not (Test-Path $OSRMPartition)) {
    Write-Host "[4/5] Running OSRM partition (MLD algorithm)..." -ForegroundColor Yellow
    Write-Host "       This step takes 2-5 minutes..." -ForegroundColor Gray

    # Format path for Podman (Windows to WSL format, e.g., C:\path -> /mnt/c/path)
    $DriveLetter = $OSRMDataDir.ToString().Substring(0, 1).ToLower()
    $RelativePath = $OSRMDataDir.ToString().Substring(2).Replace('\', '/')
    $PodmanDataDir = "/mnt/$DriveLetter$RelativePath"

    podman run --rm -v "${PodmanDataDir}:/data" osrm/osrm-backend:latest `
        osrm-partition /data/east-java-latest.osrm

    if ($LASTEXITCODE -ne 0) {
        Write-Host "       OSRM partition failed!" -ForegroundColor Red
        exit 1
    }
    Write-Host "       Partition complete!" -ForegroundColor Green
}
else {
    Write-Host "[4/5] OSRM partition data already exists." -ForegroundColor Green
}

# Step 5: OSRM Customize — Customize the graph
$OSRMCustomize = Join-Path $OSRMDataDir "east-java-latest.osrm.cell_metrics"
if (-not (Test-Path $OSRMCustomize)) {
    Write-Host "[5/5] Running OSRM customize..." -ForegroundColor Yellow

    # Format path for Podman (Windows to WSL format, e.g., C:\path -> /mnt/c/path)
    $DriveLetter = $OSRMDataDir.ToString().Substring(0, 1).ToLower()
    $RelativePath = $OSRMDataDir.ToString().Substring(2).Replace('\', '/')
    $PodmanDataDir = "/mnt/$DriveLetter$RelativePath"

    podman run --rm -v "${PodmanDataDir}:/data" osrm/osrm-backend:latest `
        osrm-customize /data/east-java-latest.osrm

    if ($LASTEXITCODE -ne 0) {
        Write-Host "       OSRM customize failed!" -ForegroundColor Red
        exit 1
    }
    Write-Host "       Customize complete!" -ForegroundColor Green
}
else {
    Write-Host "[5/5] OSRM customize data already exists." -ForegroundColor Green
}

Write-Host ""
Write-Host "============================================================" -ForegroundColor Green
Write-Host "  OSRM Setup Complete!" -ForegroundColor Green
Write-Host "============================================================" -ForegroundColor Green
Write-Host ""
Write-Host "  Data directory: $OSRMDataDir" -ForegroundColor White
Write-Host ""
Write-Host "  Next steps:" -ForegroundColor Yellow
Write-Host "    1. Start all services: podman compose up -d" -ForegroundColor White
Write-Host "    2. Test OSRM: curl http://localhost:5000/nearest/v1/driving/112.6326,-7.9666" -ForegroundColor White
Write-Host "    3. Start Go API: go run main.go" -ForegroundColor White
Write-Host ""
Write-Host "  Malang test coordinates:" -ForegroundColor Yellow
Write-Host "    Lowokwaru : -7.9666, 112.6326" -ForegroundColor Gray
Write-Host "    Blimbing  : -7.9520, 112.6550" -ForegroundColor Gray
Write-Host "    Sukun     : -8.0010, 112.6180" -ForegroundColor Gray
Write-Host "    Klojen    : -7.9780, 112.6310" -ForegroundColor Gray
Write-Host ""

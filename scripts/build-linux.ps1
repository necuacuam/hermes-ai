<#
.SYNOPSIS
  Cross-compila hermes-mcp para Linux y arma, en una carpeta aparte, todo
  lo necesario para levantarlo ahi: el binario, un .env de partida, el
  fichero de relaciones (con el contenido real ya curado en Windows, para
  no tener que volver a completarlo a mano), y un script de arranque.

.PARAMETER OutDir
  Carpeta de salida. Por defecto, "dist-linux" al lado de la carpeta
  mcp-server (mcp-server\dist-linux). No se versiona (ver .gitignore).

.PARAMETER RelacionesFile
  Nombre del fichero de relaciones a llevar (dentro de relaciones\ en el
  repo). Por defecto "relaciones.json" -- el real. Cambialo si este
  binario Linux tiene que arrancar con otro (ej. uno de otro entorno).

.PARAMETER SoloPlantilla
  Si se pasa, NO copia el fichero de relaciones real -- solo deja
  relaciones.example.json, para completar a mano en destino.

.EXAMPLE
  .\scripts\build-linux.ps1

.EXAMPLE
  .\scripts\build-linux.ps1 -OutDir C:\tmp\hermes-mcp-linux -SoloPlantilla
#>

param(
    [string]$OutDir = (Join-Path $PSScriptRoot "..\dist-linux"),
    [string]$RelacionesFile = "relaciones.json",
    [switch]$SoloPlantilla
)

$ErrorActionPreference = "Stop"

$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$OutDir = [System.IO.Path]::GetFullPath($OutDir)
$relacionesOutDir = Join-Path $OutDir "relaciones"

Write-Host "Repositorio:       $repoRoot"
Write-Host "Carpeta de salida: $OutDir"
Write-Host ""

New-Item -ItemType Directory -Force -Path $OutDir | Out-Null
New-Item -ItemType Directory -Force -Path $relacionesOutDir | Out-Null

# --- 1. Cross-compilar el binario Linux (amd64) ---
Push-Location $repoRoot
$prevGOOS = $env:GOOS
$prevGOARCH = $env:GOARCH
try {
    $env:GOOS = "linux"
    $env:GOARCH = "amd64"
    $binPath = Join-Path $OutDir "hermes-mcp"
    Write-Host "Compilando ./cmd/hermes-mcp para linux/amd64 -> $binPath"
    go build -o $binPath ./cmd/hermes-mcp
    if ($LASTEXITCODE -ne 0) {
        throw "go build fallo (exit code $LASTEXITCODE) -- revisar la salida de arriba."
    }
}
finally {
    if ($null -eq $prevGOOS) { Remove-Item Env:\GOOS -ErrorAction SilentlyContinue } else { $env:GOOS = $prevGOOS }
    if ($null -eq $prevGOARCH) { Remove-Item Env:\GOARCH -ErrorAction SilentlyContinue } else { $env:GOARCH = $prevGOARCH }
    Pop-Location
}
Write-Host "OK: binario generado."
Write-Host ""

# --- 2. .env: copiar la plantilla solo si todavia no hay una en destino ---
$envDest = Join-Path $OutDir ".env"
if (Test-Path $envDest) {
    Write-Host ".env ya existe en destino -- no se toca."
} else {
    Copy-Item (Join-Path $repoRoot ".env.example") $envDest
    Write-Host "Copiado .env.example -> .env (falta completar HERMES_MCP_MONGO_URI como minimo)."
}
Write-Host ""

# --- 3. Fichero de relaciones ---
$relacionesRealSrc = Join-Path $repoRoot "relaciones\$RelacionesFile"
$relacionesRealDest = Join-Path $relacionesOutDir $RelacionesFile
if (-not $SoloPlantilla) {
    if (Test-Path $relacionesRealSrc) {
        Copy-Item $relacionesRealSrc $relacionesRealDest -Force
        Write-Host "Copiado relaciones\$RelacionesFile (contenido real) -> relaciones\$RelacionesFile"
    } else {
        Write-Warning "No se encontro relaciones\$RelacionesFile en el repo -- solo queda la plantilla, hay que completarla a mano en destino."
    }
} else {
    Write-Host "SoloPlantilla: no se copia el fichero de relaciones real."
}
Copy-Item (Join-Path $repoRoot "relaciones\relaciones.example.json") (Join-Path $relacionesOutDir "relaciones.example.json") -Force
Write-Host ""

# --- 4. Script de arranque (run.sh) ---
# LF puro, sin BOM -- tiene que poder ejecutarse en Linux tal cual viene.
$runSh = @(
    '#!/bin/sh',
    '# Arranca hermes-mcp con HERMES_MCP_RELACIONES_PATH seteada. Esta',
    '# variable NUNCA va en .env (ver README.md del proyecto) -- tiene que',
    '# quedar seteada en el proceso que lanza el binario, por eso este',
    '# script en vez de ejecutar el binario directo.',
    'cd "$(dirname "$0")"',
    "export HERMES_MCP_RELACIONES_PATH=`"relaciones/$RelacionesFile`"",
    'exec ./hermes-mcp'
) -join "`n"
$runShPath = Join-Path $OutDir "run.sh"
[System.IO.File]::WriteAllText($runShPath, $runSh + "`n", (New-Object System.Text.UTF8Encoding($false)))
Write-Host "Generado run.sh"
Write-Host ""

Write-Host "Listo. Contenido de ${OutDir}:"
Get-ChildItem -Recurse $OutDir | ForEach-Object { Write-Host "  $($_.FullName.Substring($OutDir.Length + 1))" }

Write-Host ""
Write-Host "Pendiente YA EN LINUX, despues de copiar esta carpeta entera:"
Write-Host "  1. chmod +x hermes-mcp run.sh"
Write-Host "  2. Completar .env (HERMES_MCP_MONGO_URI como minimo)."
Write-Host "  3. Arrancar con ./run.sh"

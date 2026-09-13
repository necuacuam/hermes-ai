@echo off
REM Alternativa a run-hermes-mcp.ps1 para cuando la ejecucion de scripts de
REM PowerShell esta bloqueada por politica corporativa (GPO). Los .cmd no
REM estan sujetos a esa restriccion. Mismo patron que
REM testing-migracion-go/scripts/run-extractor.cmd.
REM
REM Uso:
REM   scripts\run-hermes-mcp.cmd
REM
REM OJO: este parser de .env es simple (split por el primer "="). Si algun
REM valor de .env contiene un "=" (ej. un query string dentro de la URI de
REM Mongo), se va a truncar. Si eso pasa, mejor usar la opcion de
REM PowerShell (run-hermes-mcp.ps1), que no tiene esa limitacion.

setlocal enabledelayedexpansion

for /f "usebackq eol=# tokens=1,* delims==" %%A in ("%~dp0..\.env") do (
    if not "%%A"=="" (
        set "%%A=%%B"
    )
)

go run "%~dp0..\cmd\hermes-mcp"

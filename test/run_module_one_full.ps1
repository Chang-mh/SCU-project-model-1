param(
    [string]$ServerUrl = "http://127.0.0.1:8080",
    [string]$ClientPython = "client/.venv/Scripts/python.exe",
    [string]$GitBash = "C:\Program Files\Git\bin\bash.exe",
    [string]$GoBin = "D:\Go\bin",
    [string]$OllamaBin = "D:\Ollama-bigmodle\Ollama",
    [switch]$RequireChatModel,
    [switch]$SkipEmbedding
)

$ErrorActionPreference = "Stop"

$repoRoot = Split-Path -Parent $PSScriptRoot
$scriptPath = Join-Path $repoRoot "test/e2e_module_one_full.sh"

if (-not (Test-Path -LiteralPath $GitBash)) {
    throw "Git Bash not found: $GitBash"
}

if (-not (Test-Path -LiteralPath $scriptPath)) {
    throw "E2E script not found: $scriptPath"
}

$env:E2E_SERVER_URL = $ServerUrl
$env:E2E_REQUIRE_EMBEDDING = if ($SkipEmbedding) { "0" } else { "1" }
$env:E2E_REQUIRE_AGENT = if ($RequireChatModel) { "1" } else { "0" }
$env:CLIENT_PYTHON = $ClientPython

$pathParts = @($GoBin, $OllamaBin, $env:Path) | Where-Object { $_ -and $_.Trim() }
$env:Path = ($pathParts -join ";")

Write-Host "[INFO] Running module one full E2E..."
Write-Host "[INFO] Server: $env:E2E_SERVER_URL"
Write-Host "[INFO] Client Python: $env:CLIENT_PYTHON"
Write-Host "[INFO] Require embedding: $env:E2E_REQUIRE_EMBEDDING"
Write-Host "[INFO] Require ChatModel enhancement: $env:E2E_REQUIRE_AGENT"

Push-Location $repoRoot
try {
    & $GitBash "test/e2e_module_one_full.sh"
    exit $LASTEXITCODE
}
finally {
    Pop-Location
}

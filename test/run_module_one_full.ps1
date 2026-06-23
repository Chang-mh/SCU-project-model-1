param(
    [string]$ServerUrl = "http://127.0.0.1:8080",
    [string]$ClientPython = "client/.venv/Scripts/python.exe",
    [string]$GitBash = "C:\Program Files\Git\bin\bash.exe",
    [string]$GoBin = "D:\Go\bin",
    [string]$OllamaBin = "D:\Ollama-bigmodle\Ollama",
    [switch]$RequireChatModel,
    [switch]$SkipEmbedding,
    [switch]$UseArkEmbedding
)

$ErrorActionPreference = "Stop"

$repoRoot = Split-Path -Parent $PSScriptRoot
$scriptName = if ($UseArkEmbedding) { "test/e2e_module_one_ark_embedding.sh" } else { "test/e2e_module_one_full.sh" }
$scriptPath = Join-Path $repoRoot $scriptName

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
Write-Host "[INFO] Script: $scriptName"
Write-Host "[INFO] Server: $env:E2E_SERVER_URL"
Write-Host "[INFO] Client Python: $env:CLIENT_PYTHON"
Write-Host "[INFO] Require embedding: $env:E2E_REQUIRE_EMBEDDING"
Write-Host "[INFO] Require ChatModel enhancement: $env:E2E_REQUIRE_AGENT"
if ($UseArkEmbedding) {
    Write-Host "[INFO] Embedding provider: Ark / 火山方舟"
    Write-Host "[INFO] Ark embedding model: $env:ARK_EMBEDDING_MODEL"
}

Push-Location $repoRoot
try {
    & $GitBash $scriptName
    exit $LASTEXITCODE
}
finally {
    Pop-Location
}

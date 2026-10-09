#!/usr/bin/env pwsh
# deploy/prod/scripts/run-security-gate.ps1
# End-to-end live security gate suite runner for AgentGate v1 production topology.

param(
    [switch]$SkipCleanRoom
)

$ErrorActionPreference = "Stop"

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$prodDir = Split-Path -Parent $scriptDir
$composeFile = Join-Path $prodDir "docker-compose.yml"
$overrideFile = Join-Path $prodDir "docker-compose.securitygate.yml"
$secretsDir = Join-Path $prodDir "secrets"

Write-Host "========================================================" -ForegroundColor Cyan
Write-Host " Starting AgentGate V1 Live Security Gate Suite" -ForegroundColor Cyan
Write-Host "========================================================" -ForegroundColor Cyan

# Step 1: Clean-room setup (unless skipped)
if (-not $SkipCleanRoom) {
    Write-Host "`n--> Step 1: Running clean-room setup" -ForegroundColor Yellow
    & (Join-Path $scriptDir "clean-room.ps1")
    if ($LASTEXITCODE -ne 0) {
        Write-Host "Clean-room verification failed; aborting security gate suite." -ForegroundColor Red
        exit 1
    }
}

# Step 2: Deploy support-desk-mcp with security gate override
Write-Host "`n--> Step 2: Applying security-gate override (DEMO_MCP_EXPOSE_LAST_CREDENTIAL_FOR_TESTS)" -ForegroundColor Yellow
& docker compose -f $composeFile -f $overrideFile up -d --build support-desk-mcp
if ($LASTEXITCODE -ne 0) {
    Write-Host "Failed to deploy support-desk-mcp with override." -ForegroundColor Red
    exit 1
}

# Wait for support-desk-mcp to be ready
Start-Sleep -Seconds 3

# Step 3: Extract secrets for test harness
$readerSecret = (Get-Content (Join-Path $secretsDir "agent_reader_secret") -Raw).Trim()
$adminSecret = (Get-Content (Join-Path $secretsDir "agent_admin_secret") -Raw).Trim()
$opsCliSecret = (Get-Content (Join-Path $secretsDir "ops_cli_client_secret") -Raw).Trim()
$adminPw = (Get-Content (Join-Path $secretsDir "admin_initial_password") -Raw).Trim()
$appPw = (Get-Content (Join-Path $secretsDir "app_password") -Raw).Trim()
$caFile = "/work/deploy/prod/secrets/ca.crt"
$dbUrl = "postgres://agentgate_app:$appPw@postgres:5432/agentgate_prod_db?sslmode=disable"

# Ensure Keycloak token endpoint is warm and responding
Write-Host "Verifying Keycloak token endpoint readiness..." -ForegroundColor Cyan
for ($i = 0; $i -lt 15; $i++) {
    $warmup = curl -k -s -o /dev/null -w "%{http_code}" -X POST https://localhost:8443/realms/agentgate/protocol/openid-connect/token `
        -H "Content-Type: application/x-www-form-urlencoded" `
        -d "grant_type=client_credentials" `
        -d "client_id=agent-reader" `
        -d "client_secret=$readerSecret"
    if ($warmup -eq "200") {
        Write-Host "Keycloak token endpoint ready." -ForegroundColor Green
        break
    }
    Start-Sleep -Seconds 1
}

# Step 4: Run Go test runner inside agentgate-prod network
Write-Host "`n--> Step 3: Running 17 v1 live security gate scenarios" -ForegroundColor Yellow

$testArgs = @(
    "run", "--rm",
    "--network", "agentgate-prod",
    "--add-host", "host.docker.internal:host-gateway",
    "-v", "/work:/work",
    "-v", "/root/go/pkg/mod:/go/pkg/mod",
    "-v", "/var/run/docker.sock:/var/run/docker.sock",
    "-w", "/work/agentgate",
    "-e", "AGENTGATE_V1_GATEWAY_URL=https://agentgateway:3000",
    "-e", "AGENTGATE_V1_BACKEND_URL=http://support-desk-mcp:9200",
    "-e", "AGENTGATE_V1_KEYCLOAK_URL=https://keycloak:8443",
    "-e", "AGENTGATE_V1_ADMIN_URL=https://agentgate:8090",
    "-e", "AGENTGATE_V1_CA_CERT=$caFile",
    "-e", "AGENTGATE_V1_READER_SECRET=$readerSecret",
    "-e", "AGENTGATE_V1_ADMIN_SECRET=$adminSecret",
    "-e", "AGENTGATE_V1_OPS_CLI_SECRET=$opsCliSecret",
    "-e", "AGENTGATE_V1_OPS_CLI_PASSWORD=$adminPw",
    "-e", "AGENTGATE_V1_DATABASE_URL=$dbUrl",
    "-e", "AGENTGATE_V1_CONTAINER_NAME=prod-agentgate",
    "-e", "AGENTGATE_V1_HOST_BACKEND_ADDR=host.docker.internal:9200",
    "golang:1.25-alpine",
    "go", "test", "-v", "-timeout", "180s", "./qa/v1security/..."
)

& docker @testArgs
$suiteExit = $LASTEXITCODE

if ($suiteExit -eq 0) {
    Write-Host "`n========================================================" -ForegroundColor Green
    Write-Host " ALL 17 V1 LIVE SECURITY GATE SCENARIOS PASSED!" -ForegroundColor Green
    Write-Host "========================================================" -ForegroundColor Green
    exit 0
} else {
    Write-Host "`n========================================================" -ForegroundColor Red
    Write-Host " V1 SECURITY GATE SUITE FAILED (exit code $suiteExit)" -ForegroundColor Red
    Write-Host "========================================================" -ForegroundColor Red
    exit $suiteExit
}
param(
    [switch]$Reset,
    [switch]$Down,
    [switch]$Volumes
)

$ErrorActionPreference = "Stop"
$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$deployDir = Resolve-Path "$scriptDir/../.."
$demoCompose = "$deployDir/prod/docker-compose.yml"
$secretsDir = "$deployDir/prod/secrets"

if ($Down) {
    if ($Volumes) {
        docker compose -f $demoCompose --profile demo down -v
    } else {
        docker compose -f $demoCompose --profile demo down
    }
    exit 0
}

Write-Host "--> Preflight check: Docker reachable?"
docker info > $null 2>&1
if ($LASTEXITCODE -ne 0) {
    Write-Error "Docker is not reachable. Is the Docker daemon running?"
}
Write-Host "Docker is reachable."

Write-Host "--> Initializing secrets (idempotent)"
& "$scriptDir/init-secrets.ps1"

Write-Host "--> Starting postgres first to let it initialize..."
docker compose -f $demoCompose --profile demo up -d postgres
Start-Sleep -Seconds 10

Write-Host "--> Starting demo-mode stack"
docker compose -f $demoCompose --profile demo up -d --build

Write-Host "--> Waiting for readiness..."
# Wait for Keycloak healthy
Write-Host "Waiting for Keycloak..."
$keycloakReady = $false
for ($i = 0; $i -lt 60; $i++) {
    $status = docker inspect --format '{{.State.Health.Status}}' prod-keycloak 2>$null
    if ($status -eq "healthy") {
        $keycloakReady = $true
        break
    }
    Start-Sleep -Seconds 2
}
if (-not $keycloakReady) {
    Write-Error "Timeout waiting for Keycloak to become healthy."
}

# Wait for AgentGate readyz
Write-Host "Waiting for AgentGate..."
$agReady = $false
for ($i = 0; $i -lt 30; $i++) {
    $status = docker inspect --format '{{.State.Health.Status}}' prod-agentgate 2>$null
    if ($status -eq "healthy") {
        $agReady = $true
        break
    }
    Start-Sleep -Seconds 2
}
if (-not $agReady) {
    Write-Error "Timeout waiting for AgentGate readyz (9443)."
}

if ($Reset) {
    Write-Host "--> Resetting state (-Reset flag provided)"

    Write-Host "1. Resetting demo backend seed data..."
    # Call the backend's reset endpoint from within the network
    docker run --rm --network agentgate-prod --entrypoint wget alpine -qO- http://support-desk-mcp:9200/_demo/reset 2>&1 | Out-Null
    
    # Authenticate to Admin API
    $adminUser = "agentgate-ops-cli"
    $adminPass = (Get-Content "$secretsDir/ops_cli_client_secret").Trim()
    
    $tokenBody = @{
        grant_type = "client_credentials"
        client_id = "agentgate-ops-cli"
        client_secret = $adminPass
    }
    $tokenResp = curl.exe -k -s -X POST https://localhost:8443/realms/agentgate/protocol/openid-connect/token -d "grant_type=client_credentials&client_id=agentgate-ops-cli&client_secret=$adminPass"
    $tokenObj = $tokenResp | ConvertFrom-Json
    $adminToken = $tokenObj.access_token
    
    if (-not $adminToken) {
        Write-Error "Failed to obtain ops-cli admin token."
    }
    
    Write-Host "2. Restoring bootstrap tool classifications..."
    $bootstrapJson = Get-Content "$deployDir/shared/tools.bootstrap.json" | ConvertFrom-Json
    foreach ($backendId in @("support-desk-mcp", "crm-system")) {
        foreach ($tool in $bootstrapJson) {
            $toolName = $tool.name
            $putBody = @{
                risk_level = $tool.risk_level
            } | ConvertTo-Json -Compress
            curl.exe -k -s -X PUT https://localhost:9443/api/v1/tools/$backendId/$toolName `
                -H "Authorization: Bearer $adminToken" `
                -H "Content-Type: application/json" `
                -d $putBody | Out-Null
        }
    }
    
    Write-Host "3. Re-activating v1.0.0 policy..."
    $rollbackBody = @{
        target_version = "v1.0.0"
    } | ConvertTo-Json -Compress
    curl.exe -k -s -X POST https://localhost:9443/api/v1/workspaces/default/policies/rollback `
        -H "Authorization: Bearer $adminToken" `
        -H "Content-Type: application/json" `
        -d $rollbackBody | Out-Null
        
    Write-Host "4. Restarting demo-agent container..."
    docker compose -f $demoCompose restart demo-agent | Out-Null
}

$adminUiPass = (Get-Content "$secretsDir/admin_initial_password").Trim()

Write-Host ""
Write-Host "================================================================" -ForegroundColor Cyan
Write-Host " AgentGate Demo Environment is Running" -ForegroundColor Cyan
Write-Host "================================================================" -ForegroundColor Cyan
Write-Host "  Admin Console : https://localhost:8444"
Write-Host "  Admin Login   : admin / $adminUiPass (local demo credential)"
Write-Host "  Admin API     : https://localhost:9443"
Write-Host "  API Gateway   : https://localhost:3443"
Write-Host "  Keycloak UI   : https://localhost:8443"
Write-Host "  Traffic Gen   : demo-agent container is running"
Write-Host "================================================================" -ForegroundColor Cyan

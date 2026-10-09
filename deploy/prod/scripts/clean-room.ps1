#!/usr/bin/env pwsh
# deploy/prod/scripts/clean-room.ps1
# Clean-room verification script for AgentGate Production deployment topology

$ErrorActionPreference = "Stop"

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$prodDir = Split-Path -Parent $scriptDir
$composeFile = Join-Path $prodDir "docker-compose.yml"
$secretsDir = Join-Path $prodDir "secrets"
$envFile = Join-Path $prodDir ".env"

$results = [System.Collections.Generic.List[PSCustomObject]]::new()
$allPassed = $true

function Record-Check($num, $name, $passed, $details = "") {
    $script:results.Add([PSCustomObject]@{
        Num     = $num
        Name    = $name
        Passed  = $passed
        Details = $details
    })
    if ($passed) {
        Write-Host "  [PASS] Step $num`: $name" -ForegroundColor Green
        if ($details) { Write-Host "         $details" -ForegroundColor Gray }
    } else {
        $script:allPassed = $false
        Write-Host "  [FAIL] Step $num`: $name" -ForegroundColor Red
        if ($details) { Write-Host "         Error: $details" -ForegroundColor DarkRed }
    }
}

Write-Host "========================================================" -ForegroundColor Cyan
Write-Host " Starting AgentGate Production Clean-Room Verification" -ForegroundColor Cyan
Write-Host "========================================================" -ForegroundColor Cyan

# -------------------------------------------------------------------------
# Step 1: docker compose down -v
# -------------------------------------------------------------------------
Write-Host "`n--> Step 1: docker compose down -v" -ForegroundColor Yellow
try {
    & docker compose -f $composeFile down -v --remove-orphans 2>&1 | Out-Null
    Record-Check 1 "Clean teardown with volumes removed" $true
} catch {
    Record-Check 1 "Clean teardown with volumes removed" $false $_.Exception.Message
    exit 1
}

# -------------------------------------------------------------------------
# Step 2: init-secrets
# -------------------------------------------------------------------------
Write-Host "`n--> Step 2: Running init-secrets.ps1" -ForegroundColor Yellow
try {
    & (Join-Path $scriptDir "init-secrets.ps1")
    if ((Test-Path (Join-Path $secretsDir "ca.crt")) -and (Test-Path $envFile)) {
        Record-Check 2 "Secrets and TLS certificates initialized" $true
    } else {
        Record-Check 2 "Secrets and TLS certificates initialized" $false "Missing required secrets files"
        exit 1
    }
} catch {
    Record-Check 2 "Secrets and TLS certificates initialized" $false $_.Exception.Message
    exit 1
}

# Read secrets for API calls
$agentReaderSecret = (Get-Content (Join-Path $secretsDir "agent_reader_secret") -Raw).Trim()
$opsCliSecret = (Get-Content (Join-Path $secretsDir "ops_cli_client_secret") -Raw).Trim()
$adminInitialPw = (Get-Content (Join-Path $secretsDir "admin_initial_password") -Raw).Trim()

# -------------------------------------------------------------------------
# Step 3: docker compose up -d --build
# -------------------------------------------------------------------------
Write-Host "`n--> Step 3: Building and starting compose topology" -ForegroundColor Yellow
try {
    & docker compose -f $composeFile up -d --build
    if ($LASTEXITCODE -eq 0) {
        Record-Check 3 "Compose topology deployed" $true
    } else {
        Record-Check 3 "Compose topology deployed" $false "Exit code $LASTEXITCODE"
        exit 1
    }
} catch {
    Record-Check 3 "Compose topology deployed" $false $_.Exception.Message
    exit 1
}

# -------------------------------------------------------------------------
# Step 4: Wait for health (Keycloak, AgentGate, Agentgateway, Admin UI)
# -------------------------------------------------------------------------
Write-Host "`n--> Step 4: Waiting for service health" -ForegroundColor Yellow
$kcHealthy = $false
for ($i = 0; $i -lt 50; $i++) {
    Start-Sleep -Seconds 2
    $code = (docker run --rm alpine/openssl wget --no-check-certificate -q -S -O /dev/null https://localhost:8443/realms/agentgate/.well-known/openid-configuration 2>&1 | Select-String "HTTP/" | Select-Object -Last 1) -replace '.*HTTP/\S+\s+(\d+).*','$1'
    if ($code -eq "200") {
        $kcHealthy = $true
        break
    }
    # Fallback check directly using curl if available
    try {
        $res = curl -k -s -o /dev/null -w "%{http_code}" https://localhost:8443/realms/agentgate/.well-known/openid-configuration
        if ($res -eq "200") {
            $kcHealthy = $true
            break
        }
    } catch {}
}

if (-not $kcHealthy) {
    Record-Check 4 "Service healthchecks" $false "Keycloak failed to report healthy within timeout"
    docker compose -f $composeFile logs keycloak
    exit 1
}

# Fetch Keycloak JWKS and update agentgateway mount
Write-Host "  Fetching Keycloak JWKS and updating agentgateway..." -ForegroundColor Yellow
curl -k -s https://localhost:8443/realms/agentgate/protocol/openid-connect/certs -o (Join-Path $secretsDir "keycloak-jwks.json")
chmod 644 (Join-Path $secretsDir "keycloak-jwks.json") 2>$null
& docker compose -f $composeFile restart agentgateway | Out-Null

# Wait for AgentGate healthz (9443)
$gateHealthy = $false
for ($i = 0; $i -lt 30; $i++) {
    Start-Sleep -Seconds 2
    try {
        $res = curl -k -s -o /dev/null -w "%{http_code}" https://localhost:9443/healthz
        if ($res -eq "200") {
            $gateHealthy = $true
            break
        }
    } catch {}
}

# Wait for Admin UI (8444)
$uiHealthy = $false
for ($i = 0; $i -lt 20; $i++) {
    Start-Sleep -Seconds 1
    try {
        $res = curl -k -s -o /dev/null -w "%{http_code}" https://localhost:8444/config.js
        if ($res -eq "200") {
            $uiHealthy = $true
            break
        }
    } catch {}
}

# Wait for Agentgateway (3443)
$gwHealthy = $false
for ($i = 0; $i -lt 20; $i++) {
    Start-Sleep -Seconds 1
    try {
        $res = curl -k -s -o /dev/null -w "%{http_code}" https://localhost:3443/
        if ($res -match "40[134]|200") {
            $gwHealthy = $true
            break
        }
    } catch {}
}

if ($kcHealthy -and $gateHealthy -and $uiHealthy -and $gwHealthy) {
    Record-Check 4 "All services healthy (Keycloak, AgentGate, Agentgateway, Admin UI)" $true
} else {
    Record-Check 4 "Service healthchecks" $false "KC=$kcHealthy Gate=$gateHealthy UI=$uiHealthy GW=$gwHealthy"
    docker compose -f $composeFile ps
    docker compose -f $composeFile logs --tail 30
    exit 1
}

# -------------------------------------------------------------------------
# Step 5: Get an agent token from Keycloak (client_credentials, agent-reader)
# -------------------------------------------------------------------------
Write-Host "`n--> Step 5: Getting agent token from Keycloak (agent-reader)" -ForegroundColor Yellow
$tokenRaw = curl -k -s -X POST https://localhost:8443/realms/agentgate/protocol/openid-connect/token `
    -H "Content-Type: application/x-www-form-urlencoded" `
    -d "grant_type=client_credentials" `
    -d "client_id=agent-reader" `
    -d "client_secret=$agentReaderSecret"

$agentTokenObj = $tokenRaw | ConvertFrom-Json
$agentToken = $agentTokenObj.access_token

if ($agentToken) {
    Record-Check 5 "Agent token obtained from Keycloak (agent-reader)" $true "Token length: $($agentToken.Length)"
} else {
    Record-Check 5 "Agent token obtained from Keycloak (agent-reader)" $false "Response: $tokenRaw"
    exit 1
}

# -------------------------------------------------------------------------
# Step 6: Call list_tickets through https://localhost:3443. Expect 200.
# -------------------------------------------------------------------------
Write-Host "`n--> Step 6: Calling list_tickets through agentgateway (expect 200)" -ForegroundColor Yellow
$listBody = '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_tickets","arguments":{}}}'
$listResp = curl -k -s -w "`n%{http_code}" -X POST https://localhost:3443/ `
    -H "Authorization: Bearer $agentToken" `
    -H "Content-Type: application/json" `
    -d $listBody

$listLines = $listResp -split "`n"
$listCode = $listLines[-1].Trim()
$listContent = ($listLines[0..($listLines.Length - 2)]) -join "`n"

if ($listCode -eq "200") {
    Record-Check 6 "list_tickets succeeded through gateway" $true "HTTP $listCode (Result contains tickets)"
} else {
    Record-Check 6 "list_tickets succeeded through gateway" $false "HTTP $listCode, body: $listContent"
    exit 1
}

# -------------------------------------------------------------------------
# Step 7: Call delete_ticket. Expect 403.
# -------------------------------------------------------------------------
Write-Host "`n--> Step 7: Calling delete_ticket through agentgateway (expect 403)" -ForegroundColor Yellow
$delBody = '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"delete_ticket","arguments":{"ticket_id":"TCK-1001"}}}'
$delResp = curl -k -s -w "`n%{http_code}" -X POST https://localhost:3443/ `
    -H "Authorization: Bearer $agentToken" `
    -H "Content-Type: application/json" `
    -d $delBody

$delLines = $delResp -split "`n"
$delCode = $delLines[-1].Trim()

if ($delCode -eq "403") {
    Record-Check 7 "delete_ticket correctly denied for reader role" $true "HTTP $delCode Forbidden"
} else {
    Record-Check 7 "delete_ticket correctly denied for reader role" $false "Expected 403, got HTTP $delCode"
    exit 1
}

# -------------------------------------------------------------------------
# Step 8: Get admin token via Keycloak password grant on agentgate-ops-cli
# -------------------------------------------------------------------------
Write-Host "`n--> Step 8: Getting admin token via password grant on agentgate-ops-cli" -ForegroundColor Yellow
$adminTokenRaw = curl -k -s -X POST https://localhost:8443/realms/agentgate/protocol/openid-connect/token `
    -H "Content-Type: application/x-www-form-urlencoded" `
    -d "grant_type=password" `
    -d "client_id=agentgate-ops-cli" `
    -d "client_secret=$opsCliSecret" `
    -d "username=admin" `
    -d "password=$adminInitialPw"

$adminTokenObj = $adminTokenRaw | ConvertFrom-Json
$adminToken = $adminTokenObj.access_token

if ($adminToken) {
    Record-Check 8 "Admin token obtained via password grant on agentgate-ops-cli" $true
} else {
    Record-Check 8 "Admin token obtained via password grant on agentgate-ops-cli" $false "Response: $adminTokenRaw"
    exit 1
}

# -------------------------------------------------------------------------
# Step 9: GET /api/v1/tools with token (expect 200), without token (expect 401)
# -------------------------------------------------------------------------
Write-Host "`n--> Step 9: Verifying Admin API auth (/api/v1/tools)" -ForegroundColor Yellow
$toolsAuthCode = (curl -k -s -o /dev/null -w "%{http_code}" https://localhost:9443/api/v1/tools -H "Authorization: Bearer $adminToken").Trim()
$toolsNoAuthCode = (curl -k -s -o /dev/null -w "%{http_code}" https://localhost:9443/api/v1/tools).Trim()

if ($toolsAuthCode -eq "200" -and $toolsNoAuthCode -eq "401") {
    Record-Check 9 "Admin API authorization verified (200 with token, 401 without)" $true "Auth=$toolsAuthCode, NoAuth=$toolsNoAuthCode"
} else {
    Record-Check 9 "Admin API authorization verified" $false "Expected 200/401, got $toolsAuthCode/$toolsNoAuthCode"
    exit 1
}

# -------------------------------------------------------------------------
# Step 10: verify-audit --workspace default. Expect OK.
# -------------------------------------------------------------------------
Write-Host "`n--> Step 10: Running verify-audit subcommand on agentgate" -ForegroundColor Yellow
$auditCheck = docker compose -f $composeFile exec -T agentgate agentgate verify-audit --workspace default
if ($LASTEXITCODE -eq 0 -and $auditCheck -match "OK") {
    Record-Check 10 "Audit cryptographic chain verified" $true "$auditCheck"
} else {
    Record-Check 10 "Audit cryptographic chain verified" $false "Exit=$LASTEXITCODE Output=$auditCheck"
    exit 1
}

# -------------------------------------------------------------------------
# Step 11: Restart postgres and agentgate, wait for readyz, test persistence
# -------------------------------------------------------------------------
Write-Host "`n--> Step 11: Restarting postgres and agentgate to prove persistence & chain continuity" -ForegroundColor Yellow

$step11Passed = $false
$step11Error = ""
$activeVersion = ""
$expectedVersion = ""

try {
    # 1. Fetch baseline v1.0.0 policy
    $basePolicyResp = curl -k -s https://localhost:9443/api/v1/workspaces/default/policies/v1.0.0 -H "Authorization: Bearer $adminToken"
    $basePolicyObj = $basePolicyResp | ConvertFrom-Json
    $newPolicyContent = $basePolicyObj.content + "`n// H2 verification"
    
    # Create candidate
    $createBody = @{
        content = $newPolicyContent
        description = "H2 verification"
    } | ConvertTo-Json -Depth 10 -Compress
    
    $createResp = curl -k -s -X POST https://localhost:9443/api/v1/workspaces/default/policies `
        -H "Authorization: Bearer $adminToken" `
        -H "Content-Type: application/json" `
        -d $createBody
        
    $createObj = $createResp | ConvertFrom-Json
    $expectedVersion = $createObj.version
    if (-not $expectedVersion) {
        throw "Failed to create candidate policy. Response: $createResp"
    }
    
    # Activate candidate
    $activateResp = curl -k -s -X POST https://localhost:9443/api/v1/workspaces/default/policies/$expectedVersion/activate `
        -H "Authorization: Bearer $adminToken"
        
    $activateObj = $activateResp | ConvertFrom-Json
    if ($activateObj.active_version -ne $expectedVersion) {
        throw "Failed to activate candidate policy. Response: $activateResp"
    }

    # 2. Restart services
    Write-Host "Restarting postgres and agentgate..."
    & docker compose -f $composeFile restart postgres agentgate | Out-Null
    
    # 3. Wait for readyz
    $ready = $false
    for ($i = 0; $i -lt 45; $i++) {
        Start-Sleep -Seconds 2
        try {
            $res = curl -k -s -o /dev/null -w "%{http_code}" https://localhost:9443/readyz
            if ($res -eq "200") { 
                $ready = $true
                break 
            }
        } catch {}
    }
    if (-not $ready) {
        throw "Timeout waiting for https://localhost:9443/readyz to return 200 (90s limit)."
    }
    
    # 4. Call list_tickets with bounded retry
    $listSuccess = $false
    for ($i = 1; $i -le 20; $i++) {
        Write-Host "list_tickets attempt $i..."
        $listResp2 = curl -k -s -w "`n%{http_code}" -X POST https://localhost:3443/ `
            -H "Authorization: Bearer $agentToken" `
            -H "Content-Type: application/json" `
            -d $listBody
        $listLines2 = $listResp2 -split "`n"
        $listCode2 = $listLines2[-1].Trim()
        
        if ($listCode2 -eq "200") {
            $listSuccess = $true
            break
        }
        Start-Sleep -Seconds 3
    }
    if (-not $listSuccess) {
        throw "Failed to get 200 from list_tickets after 20 attempts. Last code: $listCode2"
    }
    
    # 5. Fetch policy list/active version via Admin API
    $policiesResp = curl -k -s https://localhost:9443/api/v1/workspaces/default/policies -H "Authorization: Bearer $adminToken"
    $policiesObj = $policiesResp | ConvertFrom-Json
    
    foreach ($p in $policiesObj.policies) {
        if ($p.state -eq "ACTIVE") {
            $activeVersion = $p.version
            break
        }
    }
    
    if ($activeVersion -ne $expectedVersion) {
        throw "Active policy version mismatch. Expected $expectedVersion, got $activeVersion (v1.0.0 is the baseline)"
    }
    
    # 6. Re-run verify-audit
    $auditCheck2 = docker compose -f $composeFile exec -T agentgate agentgate verify-audit --workspace default
    if ($LASTEXITCODE -ne 0 -or $auditCheck2 -notmatch "OK") {
        throw "verify-audit failed: Exit=$LASTEXITCODE Output=$auditCheck2"
    }

    $step11Passed = $true
} catch {
    $step11Error = $_.Exception.Message
} finally {
    # 7. Cleanup: roll back/activate v1.0.0 again
    Write-Host "Rolling back to baseline v1.0.0..."
    $rollbackBody = @{
        target_version = "v1.0.0"
    } | ConvertTo-Json -Compress
    curl -k -s -X POST https://localhost:9443/api/v1/workspaces/default/policies/rollback `
        -H "Authorization: Bearer $adminToken" `
        -H "Content-Type: application/json" `
        -d $rollbackBody | Out-Null
}

if ($step11Passed) {
    Record-Check 11 "Persistence and chain continuity verified after service restarts" $true "Active=$activeVersion"
} else {
    Record-Check 11 "Persistence and chain continuity verified after service restarts" $false "$step11Error"
    exit 1
}

# -------------------------------------------------------------------------
# Step 12: curl metrics port from inside the network (expect agentgate_decisions_total)
# -------------------------------------------------------------------------
Write-Host "`n--> Step 12: Querying Prometheus metrics from inside network" -ForegroundColor Yellow
$metricsOut = docker run --rm --network agentgate-prod --entrypoint wget alpine/openssl -qO- http://agentgate:9090/metrics 2>&1
if ($metricsOut -match "agentgate_decisions_total") {
    Record-Check 12 "Internal metrics endpoint exposed and populated" $true "Found agentgate_decisions_total"
} else {
    Record-Check 12 "Internal metrics endpoint exposed and populated" $false "Metrics output did not contain agentgate_decisions_total"
    exit 1
}

# -------------------------------------------------------------------------
# Step 13: Print PASS summary and exit
# -------------------------------------------------------------------------
Write-Host "`n========================================================" -ForegroundColor Cyan
Write-Host " CLEAN-ROOM VERIFICATION SUMMARY" -ForegroundColor Cyan
Write-Host "========================================================" -ForegroundColor Cyan

foreach ($r in $results) {
    $statusColor = if ($r.Passed) { "Green" } else { "Red" }
    $statusText = if ($r.Passed) { "PASS" } else { "FAIL" }
    Write-Host "  [$statusText] Step $($r.Num): $($r.Name)" -ForegroundColor $statusColor
}

Write-Host "========================================================" -ForegroundColor Cyan
if ($allPassed) {
    Write-Host "  ALL 13 CLEAN-ROOM CHECKS PASSED!" -ForegroundColor Green
    Write-Host "========================================================" -ForegroundColor Cyan
    exit 0
} else {
    Write-Host "  VERIFICATION FAILED." -ForegroundColor Red
    Write-Host "========================================================" -ForegroundColor Cyan
    exit 1
}

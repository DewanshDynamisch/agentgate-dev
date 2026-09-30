# deploy/demo/run-credential-e2e.ps1
# Live proof (Docker) that the backend receives ONLY an AgentGate-minted
# downstream credential and never the agent's inbound token.
# Compatible with Windows PowerShell 5.1 and pwsh. Paths resolve from $PSScriptRoot.
param([switch]$Down)

$ErrorActionPreference = "Continue"
$demoDir    = $PSScriptRoot
$composeFile = Join-Path $demoDir "docker-compose.yml"
$secretsDir = Join-Path $demoDir "secrets"
$clientDir  = (Resolve-Path (Join-Path $demoDir "../g6/probe-client")).Path
$repoRoot   = (Resolve-Path (Join-Path $demoDir "../..")).Path
$env:GOTMPDIR = Join-Path $repoRoot ".tmp"
if (!(Test-Path $env:GOTMPDIR)) { New-Item -ItemType Directory -Path $env:GOTMPDIR -Force | Out-Null }

$results = @()
function Add-Result($name, $ok, $detail) {
    $script:results += [pscustomobject]@{ Name = $name; Ok = [bool]$ok }
    $tag = if ($ok) { "PASS" } else { "FAIL" }
    $color = if ($ok) { "Green" } else { "Red" }
    Write-Host ("[{0}] {1} - {2}" -f $tag, $name, $detail) -ForegroundColor $color
}

# Run a native command capturing stdout/stderr separately (safe on PS 5.1 and pwsh).
function Invoke-Native($file, $argString, $workDir) {
    $so = [System.IO.Path]::GetTempFileName(); $se = [System.IO.Path]::GetTempFileName()
    $p = Start-Process -FilePath $file -ArgumentList $argString -WorkingDirectory $workDir -NoNewWindow -Wait -PassThru -RedirectStandardOutput $so -RedirectStandardError $se
    $out = Get-Content -Raw $so; $err = Get-Content -Raw $se
    Remove-Item $so, $se -Force -ErrorAction SilentlyContinue
    return [pscustomobject]@{ Code = $p.ExitCode; Out = "$out"; Err = "$err" }
}

function Get-DemoState {
    $json = docker compose -f $composeFile exec -T support-desk-mcp wget -qO- http://localhost:9200/_demo/state
    return ($json | Out-String | ConvertFrom-Json)
}

function Get-Sha12($text) {
    $sha = [System.Security.Cryptography.SHA256]::Create()
    $hash = $sha.ComputeHash([System.Text.Encoding]::UTF8.GetBytes($text))
    return (([System.BitConverter]::ToString($hash)) -replace "-", "").ToLower().Substring(0, 12)
}

$script:exitCode = 1

function Invoke-Main {
    # 1. Signing key (runtime only, never committed)
    $keyPath = Join-Path $secretsDir "cred_key.pem"
    if (!(Test-Path $keyPath)) {
        Write-Host "Generating credential signing key..." -ForegroundColor Yellow
        # Dev/demo key only. chmod 644 so the (possibly non-root) agentgate container can read it through the :ro bind mount.
        docker run --rm -v "${secretsDir}:/out" --entrypoint sh alpine/openssl -c "openssl genrsa -out /out/cred_key.pem 2048 && chmod 644 /out/cred_key.pem" | Out-Null
        if (!(Test-Path $keyPath)) { Write-Host "Failed to generate signing key" -ForegroundColor Red; $script:exitCode = 1; return }
    }

    # 2. Stack up
    Write-Host "Starting demo stack (build)..." -ForegroundColor Yellow
    docker compose -f $composeFile up -d --build
    if ($LASTEXITCODE -ne 0) { Write-Host "compose up failed" -ForegroundColor Red; $script:exitCode = 1; return }

    $healthy = $false
    for ($i = 0; $i -lt 60; $i++) {
        Start-Sleep -Seconds 2
        try {
            $gate = Invoke-RestMethod -Uri "http://localhost:8091/healthz" -TimeoutSec 2
            $mcp = (docker compose -f $composeFile exec -T support-desk-mcp wget -qO- http://localhost:9200/healthz) | Out-String
            $jwks = Invoke-RestMethod -Uri "http://localhost:8091/.well-known/jwks.json" -TimeoutSec 2
            if ($gate -eq "ok" -and $mcp -match "ok" -and $jwks.keys.Count -ge 1) { $healthy = $true; break }
        } catch { }
    }
    if (-not $healthy) {
        Write-Host "Services failed to become healthy" -ForegroundColor Red
        docker compose -f $composeFile logs --tail=50
        $script:exitCode = 1; return
    }
    Write-Host "Stack healthy; JWKS published." -ForegroundColor Green

    # 3. Reset state
    docker compose -f $composeFile exec -T support-desk-mcp wget -qO- --post-data= http://localhost:9200/_demo/reset | Out-Null
    if ($LASTEXITCODE -ne 0) { Write-Host "state reset failed" -ForegroundColor Red; $script:exitCode = 1; return }

    # 4. Allowed call as reader (retry: gateway may need a moment)
    $call = $null
    for ($i = 0; $i -lt 10; $i++) {
        $call = Invoke-Native "go" "run -C `"$clientDir`" . -url http://localhost:3001 -tool list_tickets -agent-id agent-reader -roles reader -skip-init -print-token" $repoRoot
        if ($call.Code -eq 0) { break }
        Start-Sleep -Seconds 2
    }
    Add-Result "1 reader list_tickets succeeds through gateway" ($call.Code -eq 0) ("exit=" + $call.Code)
    $inbound = $null
    if ($call.Err -match "INBOUND_TOKEN=(\S+)") { $inbound = $Matches[1] }

    # 5. Backend saw minted credential, not inbound token
    $state = Get-DemoState
    $last = $null
    if ($state.invocations -and $state.invocations.Count -gt 0) { $last = $state.invocations[$state.invocations.Count - 1] }
    $subOk = ($null -ne $last) -and ($last.credential_sub -eq "agent-reader") -and ($last.PSObject.Properties.Name -contains "credential_jti") -and (-not [string]::IsNullOrEmpty($last.credential_jti))
    Add-Result "2 backend verified minted credential (sub=agent-reader)" $subOk ("credential_sub=" + $(if ($last) { $last.credential_sub } else { "<none>" }))

    $differs = $false; $detail = "no inbound token/invocation"
    if ($last -and $inbound) {
        $inboundHash = "<redacted:sha256:" + (Get-Sha12 ("Bearer " + $inbound)) + ">"
        $seen = $null
        foreach ($k in $last.headers.PSObject.Properties) { if ($k.Name -ieq "Authorization") { $seen = $k.Value } }
        Write-Host "  backend-recorded Authorization : $seen"
        Write-Host "  inbound-expected Authorization : $inboundHash"
        if ($seen -notmatch '^<redacted:sha256:[0-9a-f]{12}>$') {
            # Guard against a vacuous pass if the backend's redaction format is not what we assume.
            $detail = "backend Authorization value has unexpected format: $seen"
        } elseif ($seen -eq $inboundHash) {
            $detail = "backend received the INBOUND token (hashes equal)"
        } elseif (($state | ConvertTo-Json -Depth 10) -like "*$inbound*") {
            $detail = "inbound token found in backend record"
        } else {
            $differs = $true
            $detail = "backend=$seen inbound-expected=$inboundHash"
        }
    }
    Add-Result "3 inbound token never reaches backend" $differs $detail

    # 6. Direct backend call, no auth, from inside the network -> 401
    $body = '{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"list_tickets","arguments":{}}}'
    $b64 = [System.Convert]::ToBase64String([System.Text.Encoding]::UTF8.GetBytes($body))
    $direct = docker compose -f $composeFile exec -T agentgate sh -c "echo $b64 | base64 -d > /tmp/b.json; wget -S -O- --header=Content-Type:application/json --post-file=/tmp/b.json http://support-desk-mcp:9200/ 2>&1; true" | Out-String
    Add-Result "4 direct backend call without credential is 401" ($direct -match "HTTP/1\.[01] 401") (($direct -split "`n" | Select-String "HTTP/" | Select-Object -First 1) | Out-String).Trim()

    # 7. Reader delete_ticket -> 403, nothing mutated
    $before = Get-DemoState
    $delArgs = '-args "{\"ticket_id\":\"TCK-1001\"}"'
    $del = Invoke-Native "go" ("run -C `"$clientDir`" . -url http://localhost:3001 -tool delete_ticket $delArgs -agent-id agent-reader -roles reader -skip-init") $repoRoot
    $after = Get-DemoState
    $forbidden = (($del.Err + $del.Out) -match "403")
    $unchanged = ($after.tickets.Count -eq $before.tickets.Count) -and ($after.count -eq $before.count)
    Add-Result "5 reader delete_ticket denied (403), state unchanged" ($forbidden -and $unchanged) ("403=$forbidden tickets " + $before.tickets.Count + "->" + $after.tickets.Count + " invocations " + $before.count + "->" + $after.count)

    # 8. Audit rows carry credential ref
    $sql = "SELECT decision, reason, downstream_credential_ref FROM audit_events ORDER BY sequence_number DESC LIMIT 5"
    $rows = docker compose -f $composeFile exec -T postgres psql -U agentgate -d agentgate_demo_db -At -c $sql | Out-String
    Write-Host $rows
    $denyRow = ($rows -split "`n") | Where-Object { $_ -match "^(?i)deny" } | Select-Object -First 1
    Add-Result "6 delete_ticket denied by policy (not a malformed request)" (($null -ne $denyRow) -and ($denyRow -notmatch "malformed")) ("" + $denyRow)
    $allowRow = ($rows -split "`n") | Where-Object { $_ -match "^(?i)allow" -and $_ -match "\|jti:\S+" } | Select-Object -First 1
    Add-Result "7 ALLOW audit row has jti: downstream_credential_ref" ($null -ne $allowRow) ("" + $allowRow)

    Write-Host ""
    $failed = @($results | Where-Object { -not $_.Ok })
    if ($failed.Count -gt 0) { Write-Host ("FAILED: {0} of {1} checks" -f $failed.Count, $results.Count) -ForegroundColor Red; $script:exitCode = 1; return }
    Write-Host ("ALL {0} CHECKS PASSED" -f $results.Count) -ForegroundColor Green
    $script:exitCode = 0

}

try {
    Invoke-Main
} finally {
    # Teardown runs on every exit path when -Down is requested.
    if ($Down) { docker compose -f $composeFile down -v | Out-Null }
}
exit $script:exitCode

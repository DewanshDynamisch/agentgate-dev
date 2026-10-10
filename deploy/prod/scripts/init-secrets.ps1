#!/usr/bin/env pwsh
# deploy/prod/scripts/init-secrets.ps1
# Idempotently initializes TLS certificates, credentials, and passwords for deploy/prod

$ErrorActionPreference = "Stop"

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$prodDir = Split-Path -Parent $scriptDir
$secretsDir = Join-Path $prodDir "secrets"
$envFile = Join-Path $prodDir ".env"
$envExample = Join-Path $prodDir ".env.example"

if (-not (Test-Path $secretsDir)) {
    New-Item -ItemType Directory -Path $secretsDir -Force | Out-Null
}

Write-Host "==> Checking and initializing secrets in $secretsDir" -ForegroundColor Cyan

function Ensure-SecretFile($fileName, $generator) {
    $filePath = Join-Path $secretsDir $fileName
    if (-not (Test-Path $filePath)) {
        Write-Host "  Generating $fileName..." -ForegroundColor Yellow
        $value = & $generator
        [System.IO.File]::WriteAllText($filePath, $value.Trim() + "`n", [System.Text.Encoding]::UTF8)
    } else {
        Write-Host "  $fileName already exists, skipping." -ForegroundColor Gray
    }
    return ([System.IO.File]::ReadAllText($filePath, [System.Text.Encoding]::UTF8)).Trim()
}

# 1. Random passwords and secrets
$postgresPw = Ensure-SecretFile "postgres_password" { (docker run --rm alpine/openssl rand -hex 16).Trim() }
$migratorPw = Ensure-SecretFile "migrator_password" { (docker run --rm alpine/openssl rand -hex 16).Trim() }
$appPw = Ensure-SecretFile "app_password" { (docker run --rm alpine/openssl rand -hex 16).Trim() }
$keycloakDbPw = Ensure-SecretFile "keycloak_db_password" { (docker run --rm alpine/openssl rand -hex 16).Trim() }
$kcAdminPw = Ensure-SecretFile "kc_admin_password" { (docker run --rm alpine/openssl rand -hex 16).Trim() }
$adminInitialPw = Ensure-SecretFile "admin_initial_password" { (docker run --rm alpine/openssl rand -hex 16).Trim() }
$auditSalt = Ensure-SecretFile "audit_salt" { (docker run --rm alpine/openssl rand -hex 32).Trim() }
$readerSecret = Ensure-SecretFile "agent_reader_secret" { (docker run --rm alpine/openssl rand -hex 16).Trim() }
$adminSecret = Ensure-SecretFile "agent_admin_secret" { (docker run --rm alpine/openssl rand -hex 16).Trim() }
$opsCliSecret = Ensure-SecretFile "ops_cli_client_secret" { (docker run --rm alpine/openssl rand -hex 16).Trim() }

# 2. Downstream credential RSA signing key
$credKeyFile = Join-Path $secretsDir "cred_key.pem"
if (-not (Test-Path $credKeyFile)) {
    Write-Host "  Generating credential signing key (cred_key.pem)..." -ForegroundColor Yellow
    docker run --rm -v "${secretsDir}:/sec" alpine/openssl genpkey -algorithm RSA -out /sec/cred_key.pem -pkeyopt rsa_keygen_bits:2048 | Out-Null
} else {
    Write-Host "  cred_key.pem already exists, skipping." -ForegroundColor Gray
}

# 3. Internal Certificate Authority (CA)
$caKey = Join-Path $secretsDir "ca.key"
$caCrt = Join-Path $secretsDir "ca.crt"
if (-not (Test-Path $caKey) -or -not (Test-Path $caCrt)) {
    Write-Host "  Generating Internal CA (ca.key, ca.crt)..." -ForegroundColor Yellow
    docker run --rm -v "${secretsDir}:/sec" alpine/openssl genrsa -out /sec/ca.key 4096 | Out-Null
    docker run --rm -v "${secretsDir}:/sec" alpine/openssl req -x509 -new -nodes -key /sec/ca.key -sha256 -days 365 -out /sec/ca.crt -subj "/CN=AgentGate-Internal-CA/O=AgentGate" | Out-Null
} else {
    Write-Host "  Internal CA already exists, skipping." -ForegroundColor Gray
}

# 4. Server Certificate with SANs (agentgate, agentgateway, keycloak, admin-ui, localhost, 127.0.0.1)
$serverKey = Join-Path $secretsDir "server.key"
$serverCrt = Join-Path $secretsDir "server.crt"
if (-not (Test-Path $serverKey) -or -not (Test-Path $serverCrt)) {
    Write-Host "  Generating Server Certificate with SANs..." -ForegroundColor Yellow
    docker run --rm -v "${secretsDir}:/sec" alpine/openssl genrsa -out /sec/server.key 2048 | Out-Null
    docker run --rm -v "${secretsDir}:/sec" alpine/openssl req -new -key /sec/server.key -out /sec/server.csr -subj "/CN=AgentGate-Internal-Server/O=AgentGate" | Out-Null

    $sanExt = "authorityKeyIdentifier=keyid,issuer`nbasicConstraints=CA:FALSE`nkeyUsage=digitalSignature,keyEncipherment`nextendedKeyUsage=serverAuth`nsubjectAltName=DNS:agentgate,DNS:agentgateway,DNS:keycloak,DNS:admin-ui,DNS:localhost,IP:127.0.0.1`n"
    [System.IO.File]::WriteAllText((Join-Path $secretsDir "server.ext"), $sanExt, [System.Text.Encoding]::UTF8)

    docker run --rm -v "${secretsDir}:/sec" alpine/openssl x509 -req -in /sec/server.csr -CA /sec/ca.crt -CAkey /sec/ca.key -CAcreateserial -out /sec/server.crt -days 365 -sha256 -extfile /sec/server.ext | Out-Null
    Remove-Item (Join-Path $secretsDir "server.csr") -ErrorAction SilentlyContinue
    Remove-Item (Join-Path $secretsDir "server.ext") -ErrorAction SilentlyContinue
} else {
    Write-Host "  Server certificate already exists, skipping." -ForegroundColor Gray
}

# Copy server cert/key for individual service mounts
foreach ($svc in @("agentgate", "agentgateway", "keycloak", "admin-ui")) {
    $svcCrt = Join-Path $secretsDir "$svc.crt"
    $svcKey = Join-Path $secretsDir "$svc.key"
    if (-not (Test-Path $svcCrt)) { Copy-Item $serverCrt $svcCrt }
    if (-not (Test-Path $svcKey)) { Copy-Item $serverKey $svcKey }
}

# 5. Agentgateway Client Certificate for mTLS ext_authz
$clientKey = Join-Path $secretsDir "client.key"
$clientCrt = Join-Path $secretsDir "client.crt"
if (-not (Test-Path $clientKey) -or -not (Test-Path $clientCrt)) {
    Write-Host "  Generating agentgateway client certificate for mTLS..." -ForegroundColor Yellow
    docker run --rm -v "${secretsDir}:/sec" alpine/openssl genrsa -out /sec/client.key 2048 | Out-Null
    docker run --rm -v "${secretsDir}:/sec" alpine/openssl req -new -key /sec/client.key -out /sec/client.csr -subj "/CN=agentgateway-client/O=AgentGate" | Out-Null

    $clientExt = "authorityKeyIdentifier=keyid,issuer`nbasicConstraints=CA:FALSE`nkeyUsage=digitalSignature,keyEncipherment`nextendedKeyUsage=clientAuth`n"
    [System.IO.File]::WriteAllText((Join-Path $secretsDir "client.ext"), $clientExt, [System.Text.Encoding]::UTF8)

    docker run --rm -v "${secretsDir}:/sec" alpine/openssl x509 -req -in /sec/client.csr -CA /sec/ca.crt -CAkey /sec/ca.key -CAcreateserial -out /sec/client.crt -days 365 -sha256 -extfile /sec/client.ext | Out-Null
    Remove-Item (Join-Path $secretsDir "client.csr") -ErrorAction SilentlyContinue
    Remove-Item (Join-Path $secretsDir "client.ext") -ErrorAction SilentlyContinue
} else {
    Write-Host "  Client certificate already exists, skipping." -ForegroundColor Gray
}

# 6. Seed keycloak-jwks.json if missing (so agentgateway can validate on startup)
$jwksFile = Join-Path $secretsDir "keycloak-jwks.json"
if (-not (Test-Path $jwksFile)) {
    Write-Host "  Creating bootstrap keycloak-jwks.json..." -ForegroundColor Yellow
    [System.IO.File]::WriteAllText($jwksFile, '{"keys":[]}' + "`n", [System.Text.Encoding]::UTF8)
}

# 7. Ensure permissions: files readable by non-root containers (chmod 644)
Write-Host "  Applying permissions to secrets using Docker..." -ForegroundColor Yellow
docker run --rm -v "${secretsDir}:/sec" alpine sh -c "chmod 644 /sec/*" | Out-Null

# 8. Render .env from .env.example
if (-not (Test-Path $envFile)) {
    Write-Host "  Generating $envFile from .env.example..." -ForegroundColor Yellow
    $exampleContent = Get-Content $envExample -Raw
    $rendered = $exampleContent `
        -replace 'change-me-postgres-password', $postgresPw `
        -replace 'change-me-migrator-password', $migratorPw `
        -replace 'change-me-app-password', $appPw `
        -replace 'change-me-keycloak-password', $keycloakDbPw `
        -replace 'change-me-keycloak-admin-password', $kcAdminPw `
        -replace 'change-me-admin-initial-password', $adminInitialPw `
        -replace 'change-me-audit-salt-random-hex', $auditSalt `
        -replace 'change-me-reader-secret', $readerSecret `
        -replace 'change-me-admin-secret', $adminSecret `
        -replace 'change-me-ops-cli-secret', $opsCliSecret

    [System.IO.File]::WriteAllText($envFile, $rendered, [System.Text.Encoding]::UTF8)
    Write-Host "  .env generated successfully." -ForegroundColor Green
} else {
    Write-Host "  $envFile already exists, skipping." -ForegroundColor Gray
}

Write-Host "==> Secrets initialization complete." -ForegroundColor Green
#!/usr/bin/env bash
# path: scripts/seed_demo.sh
set -euo pipefail

API_URL="${API_URL:-http://localhost:8080}"

echo "=================================================="
echo "   Forge CI — Live Pipeline & Billing Demo Runner "
echo "   Nairobi, Kenya (Africa/Nairobi)                "
echo "=================================================="

# 1. Health Probe
echo -e "\n[1/5] Checking Forge CI API health probe..."
HEALTH_RESP=$(curl -s -f "${API_URL}/healthz" || echo "failed")
if [[ "${HEALTH_RESP}" == "failed" ]]; then
  echo "Error: Forge CI API is not running at ${API_URL}."
  echo "Start the server with: go run cmd/api/main.go"
  exit 1
fi
echo "       Response: ${HEALTH_RESP}"

# 2. Store Encrypted Secret
echo -e "\n[2/5] Storing encrypted secret (DEPLOY_TOKEN) with AES-256-GCM..."
curl -s -X POST "${API_URL}/secrets" \
  -H "Content-Type: application/json" \
  -d '{"name": "DEPLOY_TOKEN", "value": "ghp_secure_deployment_token_9876543210"}'
echo -e "\n       Secret encrypted and stored in vault."

# 3. Top-Up Compute Credits via M-Pesa STK Push
echo -e "\n[3/5] Initiating M-Pesa STK Push top-up (500.00 KES)..."
TOPUP_RESP=$(curl -s -X POST "${API_URL}/billing/mpesa/topup" \
  -H "Content-Type: application/json" \
  -d '{"phone_number": "0712345678", "amount_kes": 500.0, "user_id": "chris_nairobi"}')
echo "       M-Pesa STK Response: ${TOPUP_RESP}"

# 4. Trigger 4-Step Diamond Pipeline
echo -e "\n[4/5] Triggering 4-step diamond pipeline execution..."
PIPELINE_YAML=$(cat << 'EOF'
version: 1
name: forge-diamond-pipeline

steps:
  - id: checkout
    name: Repository Checkout
    image: alpine/git:latest
    run: git clone https://github.com/crnji/forge-ci.git .

  - id: lint
    name: Static Code Analysis
    image: golang:1.22-alpine
    run: go vet ./...
    depends-on: [checkout]

  - id: test
    name: Parallel Test Execution
    image: golang:1.22-alpine
    run: go test -v ./...
    depends-on: [checkout]

  - id: build
    name: Production Compilation & Packaging
    image: golang:1.22-alpine
    run: go build -o dist/app ./...
    depends-on: [lint, test]
    artifacts:
      - dist/**
EOF
)

RUN_RESP=$(curl -s -X POST "${API_URL}/runs" \
  -H "Content-Type: application/json" \
  -d "$(jq -n \
    --arg repo "https://github.com/crnji/forge-ci.git" \
    --arg branch "main" \
    --arg commit "a1b2c3d4e5f67890abcdef12" \
    --arg yaml "${PIPELINE_YAML}" \
    '{repo_url: $repo, branch: $branch, commit_sha: $commit, pipeline_yaml: $yaml}')")

RUN_ID=$(echo "${RUN_RESP}" | jq -r '.id')
echo "       Triggered Run ID: ${RUN_ID}"
echo "       Web UI: http://localhost:5173"

# 5. Poll for Completion
echo -e "\n[5/5] Monitoring pipeline status (polling /runs/${RUN_ID})..."
for i in {1..20}; do
  STATUS_RESP=$(curl -s "${API_URL}/runs/${RUN_ID}")
  STATUS=$(echo "${STATUS_RESP}" | jq -r '.status')
  DURATION=$(echo "${STATUS_RESP}" | jq -r '.duration_ms')

  echo "       [${i}/20] Status: ${STATUS} (${DURATION}ms elapsed)"

  if [[ "${STATUS}" == "success" || "${STATUS}" == "failure" || "${STATUS}" == "cancelled" ]]; then
    break
  fi
  sleep 1
done

echo -e "\n=================================================="
if [[ "${STATUS}" == "success" ]]; then
  echo "   Kazi imefaulu! Pipeline run completed successfully."
else
  echo "   Run completed with status: ${STATUS}"
fi
echo "=================================================="


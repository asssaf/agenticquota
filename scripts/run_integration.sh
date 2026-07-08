#!/bin/bash
set -euo pipefail

# Integration test script for agenticquota using dev_appserver.py

# 1. Check prerequisites
if ! command -v dev_appserver.py &> /dev/null; then
    echo "Error: dev_appserver.py is not installed or not in PATH."
    echo "Please install the Google Cloud SDK with the App Engine components."
    exit 1
fi

# 2. Setup mock env variables configuration for GAE dev server
# Create temporary env_variables.yaml if not present
TEMP_ENV_CREATED=false
if [ ! -f "env_variables.yaml" ]; then
    echo "Creating temporary env_variables.yaml for testing..."
    echo -e "env_variables:\n  QUOTA_API_KEY: \"test-integration-key\"" > env_variables.yaml
    TEMP_ENV_CREATED=true
fi

# Ensure clean up on exit
cleanup() {
    echo "Cleaning up..."
    if [ -n "${DEV_SERVER_PID:-}" ]; then
        echo "Stopping dev_appserver.py (PID: $DEV_SERVER_PID)..."
        kill "$DEV_SERVER_PID" || true
        wait "$DEV_SERVER_PID" 2>/dev/null || true
    fi
    if [ "$TEMP_ENV_CREATED" = true ]; then
        echo "Removing temporary env_variables.yaml..."
        rm -f env_variables.yaml
    fi
}
trap cleanup EXIT

# 3. Start dev_appserver.py in the background
PORT=8085
echo "Starting dev_appserver.py on port $PORT..."
dev_appserver.py --port=$PORT --enable_host_checking=false app.yaml &
DEV_SERVER_PID=$!

# Wait for server to start responding
echo "Waiting for server to spin up..."
for i in {1..30}; do
    if curl -s http://localhost:$PORT/_ah/health | grep -q "ok"; then
        echo "Server is healthy and ready!"
        break
    fi
    if [ "$i" -eq 30 ]; then
        echo "Error: Server failed to start in time."
        exit 1
    fi
    sleep 1
done

# 4. Perform API Integration Tests

echo "Running Integration Test Cases..."

# Case A: Get Quota (Unauthenticated) -> Should be 401 Unauthorized
echo -n "Test Case A (Unauthorized GET): "
CODE=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:$PORT/api/v1/quota)
if [ "$CODE" -eq 401 ]; then
    echo "PASS"
else
    echo "FAIL (Got status $CODE, expected 401)"
    exit 1
fi

# Case B: Get Quota (Authenticated but Empty) -> Should be 404 Not Found
echo -n "Test Case B (Authenticated GET Empty): "
CODE=$(curl -s -o /dev/null -w "%{http_code}" -H "X-API-Key: test-integration-key" http://localhost:$PORT/api/v1/quota)
if [ "$CODE" -eq 404 ]; then
    echo "PASS"
else
    echo "FAIL (Got status $CODE, expected 404)"
    exit 1
fi

# Case C: POST Quota -> Should be 200 OK
echo -n "Test Case C (Authenticated POST): "
PAYLOAD='{
  "quota": {
    "3p-5h": {
      "remaining_fraction": 0.85,
      "reset_time": "2026-07-08T10:00:52Z",
      "reset_in_seconds": 17999
    }
  }
}'
CODE=$(curl -s -o /dev/null -w "%{http_code}" \
  -X POST \
  -H "X-API-Key: test-integration-key" \
  -H "Content-Type: application/json" \
  -d "$PAYLOAD" \
  http://localhost:$PORT/api/v1/quota)
if [ "$CODE" -eq 200 ]; then
    echo "PASS"
else
    echo "FAIL (Got status $CODE, expected 200)"
    exit 1
fi

# Case D: GET Quota (Authenticated with Data) -> Should be 200 OK and match payload
echo -n "Test Case D (Authenticated GET Match): "
RESPONSE=$(curl -s -H "X-API-Key: test-integration-key" http://localhost:$PORT/api/v1/quota)
if echo "$RESPONSE" | grep -q "0.85"; then
    echo "PASS"
else
    echo "FAIL (Response did not match expected values: $RESPONSE)"
    exit 1
fi

echo "All Integration Tests Passed successfully!"

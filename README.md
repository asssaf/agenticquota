# agenticquota

A lightweight Go service designed to run in the Google App Engine (GAE) Standard Environment. It exposes a secure REST API endpoint to report (via POST) and retrieve (via GET) quota utilization metrics.

---

## Features
- **Go 1.22 Runtime**: Developed using the Go standard library with zero third-party dependencies.
- **GAE Standard Ready**: Optimized for fast startup, automatic scaling, and configuration via `app.yaml`.
- **API Key Authentication**: Protected by an `X-API-Key` header check.
- **Thread-safe Store**: In-memory state tracking to persist and fetch submitted quota metrics.

---

## Project Structure

```text
├── cmd/
│   └── server/
│       └── main.go       # Application entrypoint & routing config
├── internal/
│   ├── handler/
│   │   └── quota.go      # HTTP handlers (GET/POST) and authentication middleware
│   ├── model/
│   │   └── quota.go      # JSON schema structures
│   └── service/
│       └── quota.go      # Thread-safe in-memory store and retrieval logic
├── app.yaml              # GAE deployment configuration
├── go.mod                # Go module definition
└── README.md             # This file
```

---

## Local Development & Testing

### 1. Prerequisites
- [Go 1.22+](https://go.dev/doc/install) installed locally.

### 2. Run the Server
Set the API key environment variable and start the application:

```bash
export QUOTA_API_KEY="your-secret-api-key"
go run cmd/server/main.go
```

By default, the server runs on port `8080`.

### 3. Verify Endpoints

#### A. Health Check (Public)
```bash
curl http://localhost:8080/_ah/health
# Expected Output: ok
```

#### B. Get Quota (Unauthenticated - fails)
```bash
curl -i http://localhost:8080/api/v1/quota
# Expected Output: 401 Unauthorized
```

#### C. Get Quota (Authenticated but empty - 404)
```bash
curl -i -H "X-API-Key: your-secret-api-key" http://localhost:8080/api/v1/quota
# Expected Output: 404 Not Found
```

#### D. Report/POST Quota (Authenticated - 200 OK)
```bash
curl -i -X POST \
  -H "X-API-Key: your-secret-api-key" \
  -H "Content-Type: application/json" \
  -d '{
    "quota": {
      "3p-5h": {
        "remaining_fraction": 1.0,
        "reset_time": "2026-07-08T10:00:52Z",
        "reset_in_seconds": 17999
      },
      "gemini-5h": {
        "remaining_fraction": 0.81359,
        "reset_time": "2026-07-08T07:14:43Z",
        "reset_in_seconds": 8030
      }
    }
  }' \
  http://localhost:8080/api/v1/quota

# Expected Output: 200 OK with success JSON
```

#### E. Fetch Reported Quota (Authenticated - 200 OK)
```bash
curl -i -H "X-API-Key: your-secret-api-key" http://localhost:8080/api/v1/quota
# Expected Output: 200 OK with the POSTed JSON payload
```

---

## Deployment to Google App Engine

Deploy the application to the App Engine standard environment using the Google Cloud CLI (`gcloud`):

```bash
gcloud app deploy app.yaml --project YOUR_GCP_PROJECT_ID
```

### Setting Environment Variables securely in GAE
To supply the API key securely on GAE without checking secrets into version control:

1. Copy the template configuration file:
   ```bash
   cp env_variables.yaml.template env_variables.yaml
   ```
2. Open `env_variables.yaml` and set your desired `QUOTA_API_KEY`:
   ```yaml
   env_variables:
     QUOTA_API_KEY: "your-actual-secret-key"
   ```
3. The `app.yaml` configuration is set up to automatically merge this file during deployment using the `includes:` directive:
   ```yaml
   includes:
     - env_variables.yaml
   ```

> [!IMPORTANT]
> `env_variables.yaml` is ignored by git (configured in `.gitignore`) to ensure your secrets are never committed to your repository.

---

## Running locally with dev_appserver.py

You can run the App Engine local development server manually to test the application using the configuration files:

```bash
# 1. Make sure env_variables.yaml is configured
cp env_variables.yaml.template env_variables.yaml
# Edit env_variables.yaml to configure your QUOTA_API_KEY

# 2. Start the local development server (binds to localhost on port 8080 by default)
dev_appserver.py app.yaml

# Or customize port and bind to all interfaces (0.0.0.0) for external access:
dev_appserver.py --host=0.0.0.0 --port=8080 app.yaml
```

The server dynamically loads `app.yaml`, merges `env_variables.yaml` for configuration and environment variables, and starts the Go web service. Using `--host=0.0.0.0` makes the development server listen on all network interfaces rather than just localhost loopback.

---

## Integration Testing with dev_appserver.py

### 1. Prerequisites (Local SDK Installation)
If you do not have the Google Cloud SDK and App Engine component installed, run our helper installation script to download and install them under `~/host-cache/gcloud`:

```bash
./scripts/dev-setup.sh
```

Before running the tests, add the installed SDK binaries to your environment path:
```bash
export PATH="$HOME/host-cache/gcloud/google-cloud-sdk/bin:$PATH"
```

### 2. Run the integration tests:
```bash
./scripts/run_integration.sh
```

This script:
1. Verifies that `dev_appserver.py` is present in your command line search path.
2. Checks for or temporarily generates `env_variables.yaml`.
3. Launches the dev server in the background on port `8085`.
4. Executes curl integration calls verifying API authentication, GET behavior when empty (404), POST behavior (200), and GET matching of posted payloads.
5. Shuts down the background dev server cleanly upon completion or error.



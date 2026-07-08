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

### Setting Environment Variables in GAE
To supply the API key securely on GAE, you can add environment variables inside your local `app.yaml` file (ensure this is not committed to public repositories):

```yaml
env_variables:
  QUOTA_API_KEY: "your-secret-api-key"
```

> [!IMPORTANT]
> To prevent leaking production API keys, avoid committing sensitive secrets to source control. Instead, use a secret manager or inject them during deployment pipelines.

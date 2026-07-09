# agenticquota

A lightweight Go service designed to run in the Google App Engine (GAE) Standard Environment. It exposes a secure REST API endpoint to report (via POST) and retrieve (via GET) quota utilization metrics.

---

## Features
- **Go 1.25 Runtime**: Developed with standard GCP Monitoring client integration.
- **GAE Standard Ready**: Optimized for fast startup, automatic scaling, and configuration via `app.yaml`.
- **API Key Authentication**: Protected by an `X-API-Key` header check.
- **GCP Cloud Monitoring Integration**: Reports and retrieves quota metrics (remaining fraction, reset in seconds, reset time epoch) to Google Cloud Monitoring when the service is run with a configured Google Cloud project, falling back to a thread-safe in-memory store. Read queries are optimized with a short-term (30s) in-memory cache to prevent exceeding API free tier limits, with immediate invalidation on new writes.
- **Interactive Quota Dashboard**: A premium, lightweight web interface served at `/` featuring live countdowns, circular status gauges, and local settings memory.

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
│       └── quota.go      # GCP Monitoring reporting and retrieval with fallback store
├── app.yaml              # GAE deployment configuration
├── go.mod                # Go module definition
└── README.md             # This file
```

---

## Local Development & Testing

### 1. Prerequisites
- [Go 1.25+](https://go.dev/doc/install) installed locally.

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

### GCP Prerequisites and Permissions

To enable reporting and retrieving quota metrics via GCP Cloud Monitoring, make sure the following setup is configured on your Google Cloud Project:

1. **Enable the Cloud Monitoring API**:
   Enable the API via the Google Cloud Console or the `gcloud` CLI:
   ```bash
   gcloud services enable monitoring.googleapis.com --project YOUR_GCP_PROJECT_ID
   ```

2. **Configure IAM Permissions**:
   The service account running your App Engine application (typically the default App Engine service account `YOUR_PROJECT_ID@appspot.gserviceaccount.com` or your configured runtime service account) must be granted the following IAM roles:
   - **Monitoring Metric Writer** (`roles/monitoring.metricWriter`): Required to publish the custom metrics (via POST).
   - **Monitoring Viewer** (`roles/monitoring.viewer`): Required to query and read back the metrics (via GET).

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

# Or customize port, bind to all interfaces, and disable host verification:
dev_appserver.py --host=0.0.0.0 --port=8080 --enable_host_checking=false app.yaml
```

The server dynamically loads `app.yaml`, merges `env_variables.yaml` for environment configurations, and starts the Go web service. 
- `--host=0.0.0.0` allows binding to all network interfaces for external reachability.
- `--enable_host_checking=false` disables the host header validation check, preventing errors like `"Request Host 172.17.0.2 not whitelisted"`.

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

---

## Integration with antigravity-cli Custom Statusline

You can automatically export your CLI's real-time quota metrics to the **agenticquota** service using a custom status line script in the **Antigravity CLI** (`agy`). Whenever the agent state changes, the CLI runs your custom script and pipes agent telemetry (including the active model and quota information) as a JSON payload to `stdin`. 

Your script can extract this quota information, POST it to the **agenticquota** service in the background (which forwards it to Google Cloud Monitoring if configured), and print a formatted status line to `stdout` instantly.

### 1. Enable Custom Statusline in Settings

Open your `antigravity-cli` settings file:
- **Linux/macOS**: `~/.gemini/antigravity-cli/settings.json`
- **Windows**: `%USERPROFILE%\.gemini\antigravity-cli\settings.json`

Add or update the `statusLine` configuration to run a custom command:

```json
{
  "statusLine": {
    "type": "command",
    "command": "/path/to/your/statusline.sh",
    "enabled": true
  }
}
```

### 2. Create the Statusline Script (`statusline.sh`)

Create a script (e.g., at `/path/to/your/statusline.sh`) with the following contents, making sure to make it executable (`chmod +x statusline.sh`):

```bash
#!/bin/bash
# Read telemetry JSON from stdin provided by antigravity-cli
TELEMETRY=$(cat)

# Define your agenticquota service configuration
QUOTA_URL="http://localhost:8080/api/v1/quota"
API_KEY="your-secret-api-key"

# 1. POST the quota information to the agenticquota service in the background.
# We wrap the .quota field inside a {"quota": ...} object as expected by the API.
echo "$TELEMETRY" | jq '{"quota": .quota}' | curl -s -X POST \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d @- \
  "$QUOTA_URL" >/dev/null 2>&1 &

# 2. Render your custom status line here
# Extract details from the stdin telemetry JSON and print your desired status bar content to stdout.
MODEL=$(echo "$TELEMETRY" | jq -r '.active_model // "Gemini"')
echo "Model: $MODEL | [Your Custom Status Line Here]"
```

Make sure `jq` is installed on your system. This script processes the incoming JSON telemetry stream, publishes the metrics to your `agenticquota` service asynchronously so as not to block CLI TUI rendering, and outputs a clean status bar message.

---

## Quota Dashboard

<p float="left">
  <img width="500" alt="overview" src="https://github.com/user-attachments/assets/0a90325d-2505-4f81-a33f-4d0755538762" />
  <img width="500"  alt="history" src="https://github.com/user-attachments/assets/6e329fae-8270-4fcb-8d3b-d94eb102d4fa" />
</p>

The service includes a built-in, lightweight web dashboard to monitor your quotas visually.

### Accessing the Dashboard
- **Local Development**: Open `http://localhost:8080/` in your browser.
- **App Engine Production**: Access the root URL of your GAE deployment (e.g. `https://your-project.appspot.com/`).

### Design & Architecture
- **Direct GAE Frontend Delivery**: On Google App Engine, static files for the dashboard (`index.html`, `style.css`, `app.js`) are served directly via Google's CDN layers using custom `static_files` handlers in `app.yaml`, bypassing your Go instance to maximize performance and minimize GAE costs.
- **Local Fallback**: When running directly using `go run cmd/server/main.go`, the Go router serves the dashboard using `http.FileServer` from the local `web/` directory.
- **Frosted Glass (Glassmorphism)**: Uses an ultra-modern dark gradient interface with translucent glass panels, status-based neon glowing highlights, and responsive layouts.
- **Secure Client-Side Auth**: The dashboard prompts you for your `X-API-Key` and saves it securely in your browser's `localStorage` (optional). All requests to the API are made client-side using this key.
- **Real-time Countdowns**: The dashboard counts down to the quota reset time in real-time, auto-refreshing when a quota resets or at your chosen refresh interval (10s, 30s, 60s).

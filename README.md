# Feature Flag REST API

This project implements a Go-based REST API for feature flags with rule-based evaluations and persistent storage.

## Local Endpoints

The server exposes the following endpoints on `localhost:80`.

### Health

```bash
curl -i http://localhost:80/healthz
```

Expected response:
- `200 OK`
- JSON body: `{"status":"ok"}`

### Readiness

```bash
curl -i http://localhost:80/ready
```

Expected response:
- `200 OK`
- JSON body: `{"status":"ready"}`

### Create a feature flag

```bash
curl -i -X POST http://localhost:80/flags \
  -H "Content-Type: application/json" \
  -d '{
    "name": "new_feature",
    "default_state": false,
    "rules": [
      {
        "attribute": "subscription_tier",
        "operator": "equals",
        "value": "gold",
        "state": true
      }
    ]
  }'
```

Expected response:
- `201 Created`
- JSON body with the created flag

### List all feature flags

```bash
curl -i http://localhost:80/flags
```

Expected response:
- `200 OK`
- JSON array of stored feature flags

### Get a single feature flag

```bash
curl -i http://localhost:80/flags/new_feature
```

Expected response:
- `200 OK` if the flag exists
- JSON body with the feature flag details

### Update a feature flag

```bash
curl -i -X PUT http://localhost:80/flags/new_feature \
  -H "Content-Type: application/json" \
  -d '{
    "default_state": false,
    "rules": [
      {
        "attribute": "region",
        "operator": "equals",
        "value": "us-west",
        "state": true
      }
    ]
  }'
```

Expected response:
- `200 OK`
- JSON body with the updated flag

### Delete a feature flag

```bash
curl -i -X DELETE http://localhost:80/flags/new_feature
```

Expected response:
- `204 No Content`

### Evaluate a feature flag

```bash
curl -i -X POST http://localhost:80/evaluate \
  -H "Content-Type: application/json" \
  -d '{
    "flag_name": "new_feature",
    "context": {
      "user_id": "123",
      "subscription_tier": "gold",
      "region": "us-west",
      "attributes": {
        "custom_attribute": "value"
      }
    }
  }'
```

Expected response:
- `200 OK`
- JSON body containing `enabled` and `reason`

## Build and Run

To build and test the Docker image using the existing Dockerfile:

```bash
cd /workspaces
docker build -f docker/dockerfile.web -t feature-flag-server:latest .
```

Then run the container locally:

```bash
docker run -p 80:80 feature-flag-server:latest
```

## Notes

- The flag store and user context data are persisted in PostgreSQL using `DATABASE_URL` from `.env`.
- The server will create the `flags`, `users`, and `evaluations` tables automatically on startup.
- The evaluation endpoint uses rule matching before falling back to the flag's `default_state`.

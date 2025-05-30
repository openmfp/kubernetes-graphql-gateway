# ClusterAccess Feature

## Overview

The ClusterAccess feature allows the Kubernetes GraphQL Gateway listener to generate OpenAPI schemas for multiple target clusters instead of just the starting cluster. This enables multi-cluster schema generation and management.

## How it Works

### Before (Standard Flow)
1. Listener connects to the starting cluster (via KUBECONFIG)
2. Generates OpenAPI schema for the starting cluster
3. Stores schema as `kubernetes` file

### After (ClusterAccess Flow)
1. Listener connects to the starting cluster (via KUBECONFIG)
2. Lists all `ClusterAccess` resources from the starting cluster
3. For each `ClusterAccess` resource:
   - Extracts connection information (host, auth, ca)
   - Connects to the target cluster
   - Generates OpenAPI schema for the target cluster
   - Stores schema using the cluster name (from `path` field or resource name)

## ClusterAccess CRD

The `ClusterAccess` CRD defines how to connect to target clusters:

```yaml
apiVersion: gateway.openmfp.org/v1alpha1
kind: ClusterAccess
metadata:
  name: my-cluster-access
spec:
  path: kubernetes # optional, if not set, the name of the resource is used
  host: https://my-cluster.com
  ca:
    secretRef:
      name: ca-secret
      namespace: default
      key: ca.crt
  auth:
    secretRef:
      name: my-secret
      namespace: my-namespace
      key: token
```

### Fields

- **`path`** (optional): The name to use for the schema file. If not set, uses the resource name.
- **`host`** (required): The URL of the target cluster.
- **`ca`** (optional): CA certificate configuration for TLS verification.
- **`auth`** (optional): Authentication configuration for the target cluster.

## Current Implementation Status

✅ **Completed:**
- ClusterAccess CRD definition
- Go types for ClusterAccess
- Modified listener to use ClusterAccess resources
- Basic connection logic (host extraction)

🚧 **TODO (Future Iterations):**
- CA certificate handling from secrets/configmaps
- Authentication implementation (token, kubeconfig, service account)
- Error handling and retry logic
- Status reporting on ClusterAccess resources

## Usage

1. Apply the ClusterAccess CRD to your starting cluster:
   ```bash
   kubectl apply -f config/crd/gateway.openmfp.org_clusteraccess.yaml
   ```

2. Create ClusterAccess resources for your target clusters:
   ```bash
   kubectl apply -f examples/clusteraccess-example.yaml
   ```

3. Start the listener (it will automatically discover and process ClusterAccess resources):
   ```bash
   KUBECONFIG=<path-to-starting-cluster> ./bin/listener listener
   ```

The listener will generate schema files for each target cluster defined in the ClusterAccess resources. 
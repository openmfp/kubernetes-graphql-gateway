# ClusterAccess Examples

This directory contains examples of how to use the ClusterAccess CRD with different authentication methods for the Kubernetes GraphQL Gateway.

## Files

- `clusteraccess-example.yaml` - Basic example using token-based authentication
- `clusteraccess-kubeconfig-example.yaml` - **Working example** using kubeconfig authentication for a kind cluster
- `clusteraccess-multiple-clusters.yaml` - Examples for multiple clusters (may contain outdated kubeconfig data)

## Authentication Methods

The ClusterAccess CRD supports several authentication methods:

### 1. Kubeconfig Authentication (recommended for local development)
```yaml
auth:
  kubeconfigSecretRef:
    name: kubeconfig-secret
    namespace: default
```

### 2. Token-based Authentication 
```yaml
auth:
  secretRef:
    name: my-secret
    namespace: my-namespace
    key: token
```

### 3. Client Certificate Authentication
```yaml
auth:
  clientCertificateRef:
    name: cert-secret
    namespace: default
```

### 4. Service Account Authentication
```yaml
auth:
  serviceAccount: my-service-account
```

## Quick Start

1. **Deploy the working example:**
```bash
kubectl apply -f clusteraccess-kubeconfig-example.yaml
```

2. **Start the gateway and listener:**
```bash
# Terminal 1: Start listener (processes ClusterAccess resources)
task listener

# Terminal 2: Start gateway (serves GraphQL endpoints)
task gateway
```

3. **Access your cluster via GraphQL:**
   - The `kind-kind-cluster` will be accessible at `http://localhost:8080/kind/graphql`
   - Each cluster gets its own endpoint based on the `path` field

## How It Works

1. **ClusterAccess CRD**: Defines connection details for target clusters
2. **Listener**: Reads ClusterAccess resources and generates OpenAPI schemas
3. **Gateway**: Creates GraphQL endpoints for each cluster and routes requests appropriately

## Security Notes

- Store sensitive data (certificates, keys, tokens) in Kubernetes Secrets
- Use `stringData` for readable content, `data` for base64-encoded content  
- Ensure proper RBAC permissions for accessing secrets
- Consider using different namespaces for isolation 
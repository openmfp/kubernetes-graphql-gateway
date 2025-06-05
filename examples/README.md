# ClusterAccess Examples

This directory contains examples of how to use the ClusterAccess CRD with different authentication methods.

## Files

- `clusteraccess-example.yaml` - Basic example using token-based authentication
- `clusteraccess-kubeconfig-example.yaml` - Example using kubeconfig authentication for a single cluster
- `clusteraccess-multiple-clusters.yaml` - Examples for multiple clusters using kubeconfig authentication

## Authentication Methods

The ClusterAccess CRD supports several authentication methods:

### 1. Token-based Authentication (existing example)
```yaml
auth:
  secretRef:
    name: my-secret
    namespace: my-namespace
    key: token
```

### 2. Kubeconfig Authentication (recommended for complex setups)
```yaml
auth:
  kubeconfigSecretRef:
    name: kubeconfig-secret
    namespace: default
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

## Usage

1. Create the Secret containing your kubeconfig:
```bash
kubectl apply -f clusteraccess-kubeconfig-example.yaml
```

2. The ClusterAccess resource will be available at the specified path:
   - `kind-openmfp-cluster` will be accessible at `/openmfp`
   - `kind-kind-cluster` will be accessible at `/kind`
   - `first-target-cluster` will be accessible at `/target`

## Security Notes

- Store sensitive data (certificates, keys, tokens) in Kubernetes Secrets
- Use `stringData` for readable content, `data` for base64-encoded content
- Ensure proper RBAC permissions for accessing secrets
- Consider using different namespaces for isolation 
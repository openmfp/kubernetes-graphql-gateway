#!/bin/bash

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Default values
CLUSTER_NAME=""
TARGET_KUBECONFIG=""
MANAGEMENT_KUBECONFIG="${KUBECONFIG:-$HOME/.kube/config}"
SERVICE_ACCOUNT_NAME="gateway-reader"
NAMESPACE="default"
TOKEN_DURATION="24h"

usage() {
    echo "Usage: $0 --cluster-name <name> --target-kubeconfig <path> [options]"
    echo ""
    echo "Required:"
    echo "  --cluster-name <name>           Name for the ClusterAccess resource"
    echo "  --target-kubeconfig <path>      Path to target cluster kubeconfig"
    echo ""
    echo "Optional:"
    echo "  --management-kubeconfig <path>  Path to management cluster kubeconfig (default: \$KUBECONFIG or ~/.kube/config)"
    echo "  --service-account <name>        Service account name (default: gateway-reader)"
    echo "  --namespace <name>              Namespace for secrets (default: default)"
    echo "  --token-duration <duration>     Token duration (default: 24h)"
    echo "  --help                          Show this help message"
    echo ""
    echo "Example:"
    echo "  $0 --cluster-name my-cluster --target-kubeconfig ~/.kube/target-config"
}

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --cluster-name)
            CLUSTER_NAME="$2"
            shift 2
            ;;
        --target-kubeconfig)
            TARGET_KUBECONFIG="$2"
            shift 2
            ;;
        --management-kubeconfig)
            MANAGEMENT_KUBECONFIG="$2"
            shift 2
            ;;
        --service-account)
            SERVICE_ACCOUNT_NAME="$2"
            shift 2
            ;;
        --namespace)
            NAMESPACE="$2"
            shift 2
            ;;
        --token-duration)
            TOKEN_DURATION="$2"
            shift 2
            ;;
        --help)
            usage
            exit 0
            ;;
        *)
            log_error "Unknown option: $1"
            usage
            exit 1
            ;;
    esac
done

# Validate required arguments
if [[ -z "$CLUSTER_NAME" ]]; then
    log_error "Cluster name is required"
    usage
    exit 1
fi

if [[ -z "$TARGET_KUBECONFIG" ]]; then
    log_error "Target kubeconfig path is required"
    usage
    exit 1
fi

# Validate files exist
if [[ ! -f "$TARGET_KUBECONFIG" ]]; then
    log_error "Target kubeconfig file not found: $TARGET_KUBECONFIG"
    exit 1
fi

if [[ ! -f "$MANAGEMENT_KUBECONFIG" ]]; then
    log_error "Management kubeconfig file not found: $MANAGEMENT_KUBECONFIG"
    exit 1
fi

log_info "Creating ClusterAccess resource '$CLUSTER_NAME'"
log_info "Target kubeconfig: $TARGET_KUBECONFIG"
log_info "Management kubeconfig: $MANAGEMENT_KUBECONFIG"

# Extract server URL from target kubeconfig
log_info "Extracting server URL from target kubeconfig..."
SERVER_URL=$(KUBECONFIG="$TARGET_KUBECONFIG" kubectl config view --minify -o jsonpath='{.clusters[0].cluster.server}')
if [[ -z "$SERVER_URL" ]]; then
    log_error "Failed to extract server URL from kubeconfig"
    exit 1
fi
log_info "Server URL: $SERVER_URL"

# Extract CA certificate from target kubeconfig
log_info "Extracting CA certificate from target kubeconfig..."
CA_DATA=$(KUBECONFIG="$TARGET_KUBECONFIG" kubectl config view --raw --minify -o jsonpath='{.clusters[0].cluster.certificate-authority-data}')
if [[ -z "$CA_DATA" ]]; then
    log_error "Failed to extract CA certificate from kubeconfig"
    exit 1
fi

# Decode CA certificate to verify it's valid
CA_CERT=$(echo "$CA_DATA" | base64 -d)
if [[ ! "$CA_CERT" =~ "BEGIN CERTIFICATE" ]]; then
    log_error "Invalid CA certificate format"
    exit 1
fi
log_info "CA certificate extracted successfully"

# Test target cluster connectivity
log_info "Testing target cluster connectivity..."
if ! KUBECONFIG="$TARGET_KUBECONFIG" kubectl cluster-info &>/dev/null; then
    log_error "Cannot connect to target cluster"
    exit 1
fi
log_info "Target cluster is accessible"

# Create service account in target cluster
log_info "Creating service account '$SERVICE_ACCOUNT_NAME' in target cluster..."
KUBECONFIG="$TARGET_KUBECONFIG" kubectl create serviceaccount "$SERVICE_ACCOUNT_NAME" --namespace="$NAMESPACE" --dry-run=client -o yaml | \
KUBECONFIG="$TARGET_KUBECONFIG" kubectl apply -f -

# Create cluster role binding
log_info "Creating cluster role binding for service account..."
KUBECONFIG="$TARGET_KUBECONFIG" kubectl create clusterrolebinding "${SERVICE_ACCOUNT_NAME}-binding" \
    --clusterrole=view \
    --serviceaccount="${NAMESPACE}:${SERVICE_ACCOUNT_NAME}" \
    --dry-run=client -o yaml | \
KUBECONFIG="$TARGET_KUBECONFIG" kubectl apply -f -

# Generate token
log_info "Generating token for service account..."
TOKEN=$(KUBECONFIG="$TARGET_KUBECONFIG" kubectl create token "$SERVICE_ACCOUNT_NAME" --namespace="$NAMESPACE" --duration="$TOKEN_DURATION")
if [[ -z "$TOKEN" ]]; then
    log_error "Failed to generate token"
    exit 1
fi
log_info "Token generated successfully"

# Test token permissions
log_info "Testing token permissions..."
if ! KUBECONFIG="$TARGET_KUBECONFIG" kubectl auth can-i list configmaps --as="system:serviceaccount:${NAMESPACE}:${SERVICE_ACCOUNT_NAME}" &>/dev/null; then
    log_warn "Token may not have sufficient permissions to list configmaps"
fi

# Test management cluster connectivity
log_info "Testing management cluster connectivity..."
if ! KUBECONFIG="$MANAGEMENT_KUBECONFIG" kubectl cluster-info &>/dev/null; then
    log_error "Cannot connect to management cluster"
    exit 1
fi
log_info "Management cluster is accessible"

# Create token secret in management cluster
log_info "Creating token secret in management cluster..."
KUBECONFIG="$MANAGEMENT_KUBECONFIG" kubectl create secret generic "${CLUSTER_NAME}-token" \
    --namespace="$NAMESPACE" \
    --from-literal=token="$TOKEN" \
    --dry-run=client -o yaml | \
KUBECONFIG="$MANAGEMENT_KUBECONFIG" kubectl apply -f -

# Create CA secret in management cluster
log_info "Creating CA secret in management cluster..."
echo "$CA_CERT" | KUBECONFIG="$MANAGEMENT_KUBECONFIG" kubectl create secret generic "${CLUSTER_NAME}-ca" \
    --namespace="$NAMESPACE" \
    --from-file=ca.crt=/dev/stdin \
    --dry-run=client -o yaml | \
KUBECONFIG="$MANAGEMENT_KUBECONFIG" kubectl apply -f -

# Create ClusterAccess resource
log_info "Creating ClusterAccess resource..."
cat <<EOF | KUBECONFIG="$MANAGEMENT_KUBECONFIG" kubectl apply -f -
apiVersion: gateway.openmfp.org/v1alpha1
kind: ClusterAccess
metadata:
  name: $CLUSTER_NAME
spec:
  path: $CLUSTER_NAME
  host: $SERVER_URL
  ca:
    secretRef:
      name: ${CLUSTER_NAME}-ca
      namespace: $NAMESPACE
      key: ca.crt
  auth:
    secretRef:
      name: ${CLUSTER_NAME}-token
      namespace: $NAMESPACE
      key: token
EOF

log_info "ClusterAccess resource '$CLUSTER_NAME' created successfully!"
echo ""
log_info "Summary:"
echo "  - Service account: $NAMESPACE/$SERVICE_ACCOUNT_NAME (in target cluster)"
echo "  - Token secret: $NAMESPACE/${CLUSTER_NAME}-token (in management cluster)"
echo "  - CA secret: $NAMESPACE/${CLUSTER_NAME}-ca (in management cluster)"
echo "  - ClusterAccess: $CLUSTER_NAME"
echo "  - Server URL: $SERVER_URL"
echo ""
log_info "You can now run the listener to generate the schema:"
echo "  export ENABLE_KCP=false"
echo "  export LOCAL_DEVELOPMENT=false"
echo "  export KUBECONFIG=\"$MANAGEMENT_KUBECONFIG\""
echo "  task listener" 
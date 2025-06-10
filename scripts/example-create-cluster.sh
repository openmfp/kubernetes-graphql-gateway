#!/bin/bash

# Example script showing how to use the create-clusteraccess.sh script
# This demonstrates the typical workflow for setting up ClusterAccess resources

set -e

echo "🚀 ClusterAccess Setup Example"
echo "==============================="
echo ""

# Example 1: Basic usage
echo "📋 Example 1: Basic ClusterAccess creation"
echo "./scripts/create-clusteraccess.sh \\"
echo "  --cluster-name my-target-cluster \\"
echo "  --target-kubeconfig ~/.kube/target-config"
echo ""

# Example 2: Full options
echo "📋 Example 2: Full options"
echo "./scripts/create-clusteraccess.sh \\"
echo "  --cluster-name production-cluster \\"
echo "  --target-kubeconfig ~/.kube/production-config \\"
echo "  --management-kubeconfig ~/.kube/management-config \\"
echo "  --service-account production-gateway-reader \\"
echo "  --namespace kube-system \\"
echo "  --token-duration 168h"
echo ""

# Example 3: Multiple clusters
echo "📋 Example 3: Setting up multiple clusters"
echo "# Development cluster"
echo "./scripts/create-clusteraccess.sh \\"
echo "  --cluster-name dev-cluster \\"
echo "  --target-kubeconfig ~/.kube/dev-config"
echo ""
echo "# Staging cluster"
echo "./scripts/create-clusteraccess.sh \\"
echo "  --cluster-name staging-cluster \\"
echo "  --target-kubeconfig ~/.kube/staging-config"
echo ""
echo "# Production cluster"
echo "./scripts/create-clusteraccess.sh \\"
echo "  --cluster-name prod-cluster \\"
echo "  --target-kubeconfig ~/.kube/prod-config"
echo ""

echo "🔧 After creating ClusterAccess resources:"
echo "1. Run the listener to generate schemas:"
echo "   export ENABLE_KCP=false"
echo "   export LOCAL_DEVELOPMENT=false"
echo "   task listener"
echo ""
echo "2. Start the gateway:"
echo "   task gateway"
echo ""
echo "3. Test with GraphQL queries:"
echo "   curl 'http://localhost:7080/CLUSTER_NAME/graphql' \\"
echo "     -H 'Content-Type: application/json' \\"
echo "     --data-raw '{\"query\":\"{core{ConfigMaps{metadata{name}}}}\"}}'"
echo ""

echo "✅ All examples shown above!"
echo "💡 Use --help for more options: ./scripts/create-clusteraccess.sh --help" 
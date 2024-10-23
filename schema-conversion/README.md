# POC: Kubernetes API schema conversion to GraphQL

## Local setup

```bash
kind create cluster
kind export kubeconfig
kubectl get --raw /openapi/v2 > k8s-schema-v2.json
npm i -g openapi-to-graphql-cli
openapi-to-graphql k8s-schema-v2.json --save k8s-schema.graphql
```

To copy the API schema from the [Pod running inside of the cluster](./manifests/init-container.yaml), run the following command:

```bash
kubectl cp -c=busybox \
kubectl-openapi-fetch:/data/openapi-schema-v2.json \
./openapi-schema-v2.json
```

## Samples

- [K8s schema](k8s-schema-v2.json)
- [GrapqhQL schema](k8s-schema.graphql)

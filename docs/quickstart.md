# Quick start

[back to the main readme](../README.md)

## Prerequisites
- [Go](https://go.dev/doc/install)
- [Taskfile](https://taskfile.dev/#/installation)

You also may need a running Kubernetes cluster(either local or remote).

You can use:
- Option A: [KCP](https://docs.kcp.io/kcp/main/setup/quickstart/)
- Option B: standard Kubernetes cluster(e.g. [kind](https://kind.sigs.k8s.io/), minikube, etc.)

## Usage
1. Clone the repo and change to the directory:
```shell
git clone git@github.com:openmfp/kubernetes-graphql-gateway.git && cd kubernetes-graphql-gateway
```
2. Setup the environment:
```shell
# this will disable authorization
LOCAL_DEVELOPMENT=true 

# kcp is enabled by default, in case you want to run it against a standard kubernetes cluster:
ENABLE_KCP=false

# you must point to the kubeconfig of the cluster you want to run against:
KUBECONFIG=YOUR_KUBECONFIG_PATH
```
3. Run the Listener:
```shell
task listener
```
This command must result in the files being created in the `./bin/definitions` directory.
Each file corresponds to a workspace in KCP or a standard Kubernetes cluster.

Gateway will watch the directory for changes and update the schema accordingly.

4. Run the Gateway:
```shell
task gateway
```
Check the console output for the URLs of the GraphQL endpoints.

For each file in the `./bin/definitions` directory, a separate GraphQL endpoint will be created.

## Queries examples

For both KCP and standard kubernetes clusters you can use [configMap queries](./configmap_queries.md)

If you use a standard kubernetes cluster, you can use [pod queries](./pod_queries.md)

## Custom queries

Aside from queries and mutations that represent the CRUD operations, the Gateway also has [custom queries](./custom_queries.md)

## Subscriptions

You can subscribe to events using the following instructions:
- [Subscriptions](./subscriptions.md)


## Authorization

If you run the gateway with `LOCAL_DEVELOPMENT=false`, you need to add the `Authorization` header:
```shell
{
  "Authorization": "YOUR_TOKEN"
}
```

[back to the main readme](../README.md)
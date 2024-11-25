> [!WARNING]
> This repository is under construction and not yet ready for public consumption. Please check back later for updates.

# Native GQL Gateway

Native GQL Gateway adds a support of Graphql queries and mutations for native resources.

It expects a directory as an input to watch for files which will contain OpenAPI spec for the resources.

Each file in that directory will correspond to a KCP workspace(or API server)

For each single file it will create a separate URL like `/<workspace-name>/graphql` which will be used to query the resources of that workspace.

And it will be watching for changes in the directory and update the schema accordingly.

## Usage

! Note, that for now it acts a standalone cobra command, and it is not integrated with CRD gateway.

```shell
task native
```
OR
```shell
go run main.go native --watched-dir=./definitions
# where ./definitions is the directory containing the OpenAPI spec files
```

## Components overview

### Workspace manager

Holds the logic of watching a directory, triggers the schema generation and binds it to a http handler.

P.S. We are going to have an Event Listener which will watch the KCP workspace and write the OpenAPI spec into that directory.

### Gateway

Is responsible for the conversion from OpenAPI spec into the graqphl schema.

### Resolver

Holds the logic of interaction with the cluster.

## Testing

```shell
task test
```

## Subscriptions

To subscribe events you should use SSE(Server Sent Events) protocol. 
Since graphQL playground doesn't support it, you should use curl.

For instance, to subscribe to deployments changes:
```
curl -H "Accept: text/event-stream" -H "Content-Type: application/json" http://localhost:3000/fullSchema/subscriptions \
-d '{"query": "subscription { apps_deployments(namespace: \"default\") { metadata { name } spec { replicas } } }"}'
```
P.S. Any deployment's change will fire the event, so if you are interested in a specific field change, 
it should be handled on a client(applicaiton) side

```graphql

# crd-gql-gateway

The goal of this library is to provide a reusable and generic way of exposing Custom Resource Definitions from within a cluster using GraphQL. This enables UIs that need to consume these objects to do so in a developer-friendly way, leveraging a rich ecosystem.

For each registered CRD, the gateway provides the following:

- A list query that allows the client to request a list of specific CRDs based on label selectors and/or namespace.
- A query for an individual resource.
- Create/Update/Delete mutations.
- A list subscription type that opens a watch and serves the client live updates from CRDs within the cluster.

Additionally, the gateway ensures that client requests are authorized to perform the desired actions using `SubjectAccessReview`, which ensures proper authorization.

## Usage

The goal is to provide a reusable library that can serve Custom Resources from any cluster without being specifically tied to a cluster/setup. The library is also able to dynamically infer which custom resource to expose based on the registered types in the [`runtime.Scheme`](https://pkg.go.dev/k8s.io/apimachinery/pkg/runtime#Scheme), which need to be registered anyway in order to get a functioning `controller-runtime` client.

To get started, you can consume the library in the following way:

#### 1. Create a `controller-runtime.Client` however you like

Please make sure to also include the `k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1` and the `k8s.io/api/authorization/v1` types, so the library can create `SubjectAccessReviews` and load `CustomResourceDefinitions`.

```go
schema := runtime.NewScheme()
apiextensionsv1.AddToScheme(schema)
authzv1.AddToScheme(schema)
```

After you set up the generally needed schema, feel free to add the types of any CRD that is available in your target cluster to the scheme. For every type that you register, there will be a set of queries, mutations, and subscriptions generated to expose your type via the gateway.

```go
package main

import (
    // ...
    accountv1alpha1 "github.com/openmfp/account-operator/api/v1alpha1"
    // ...
)

func main() {
    // ...
    accountv1alpha1.AddToScheme(schema)

    cfg := controllerruntime.GetConfigOrDie()

    cl, err := client.NewWithWatch(cfg, client.Options{
        Scheme: schema,
    })
    if err != nil {
        panic(err)
    }
}
```

#### 2. Pass the client to the gateway library and see your resource being exposed :rocket:

```go
gqlSchema, err := gateway.New(cmd.Context(), gateway.Config{
    Client: cl,
})
if err != nil {
    return err
}

http.Handle("/graphql", gateway.Handler(gateway.HandlerConfig{
    Config: &handler.Config{
        Schema:     &gqlSchema,
        Pretty:     true,
        Playground: true,
    },
    UserClaim: "mail",
}))
```

You can expose the `gateway.Handler()` via the normal `net/http` package.

It takes care of serving the right protocol based on the `Content-Type` header, as it exposes the `subscriptions` via the [`SSE`](https://html.spec.whatwg.org/multipage/server-sent-events.html) standard.



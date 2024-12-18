> [!WARNING]
> This repository is under construction and not yet ready for public consumption. Please check back later for updates.

# GQL Gateway 

The goal of this library is to provide a reusable and generic way of exposing k8s resources from within a cluster using GraphQL.
This enables UIs that need to consume these objects to do so in a developer-friendly way, leveraging a rich ecosystem.

## Overview
GQL Gateway expects a directory as input to watch for files containing OpenAPI specifications with resources.

Each file in that directory will correspond to a KCP workspace (or API server).

For each file it will create a separate URL like `/<workspace-name>/graphql` which will be used to query the resources of that workspace.

It will be watching for changes in the directory and update the schema accordingly.

## Custom Resource support

As long as CR is registered in cluster and has 

### Usage

Your kubeconfig should point to a cluster you want to interact.

#### OpenAPI Spec

You can run the gateway using the existing generic OpenAPI spec file which is located in the `./definitions` directory.

(Optional) Or you can generate a new one from your own cluster by running the following command:
```shell
kubectl get --raw /openapi/v2 > fullSchema
```
#### Start the Service 
```shell
task start
```
OR
```shell
go run main.go start --watched-dir=./definitions
# where ./definitions is the directory containing the OpenAPI spec files
```
#### Sending queries

##### Create a Pod:

```shell
mutation {
  core {
    createPod(
      namespace: "default",
      object: {
        metadata: {
          name: "my-new-pod",
          labels: {
            app: "my-app"
          }
        }
        spec: {
          containers: [
            {
              name: "nginx-container"
              image: "nginx:latest"
              ports: [
                {
                  containerPort: 80
                }
              ]
            }
          ]
          restartPolicy: "Always"
        }
      }
    ) {
      metadata {
        name
        namespace
        labels
      }
      spec {
        containers {
          name
          image
          ports {
            containerPort
          }
        }
        restartPolicy
      }
      status {
        phase
      }
    }
  }
}
```

##### Get the created Pod:
```shell
query {
  core {
    Pod(name:"my-new-pod", namespace:"default") {
      metadata {
        name
      }
      spec{
        containers {
          image
          ports {
            containerPort
          }
        }
      }
    }
  }
}
```

##### Delete the created Pod:
```shell
mutation {
  core {
    deletePod(
      namespace: "default",
      name: "my-new-pod"
    )
  }
}
```
### Components Overview

#### Workspace manager

Holds the logic for watching a directory, triggering schema generation, and binding it to an HTTP handler.

*P.S. We are going to have an Event Listener that will watch the KCP workspace and write the OpenAPI spec into that directory.*

#### Gateway

Is responsible for the conversion from OpenAPI spec into the GraphQL schema.

#### Resolver

Holds the logic of interaction with the cluster.

### Testing

```shell
task test
```

If you want to run single test, you need to export a KUBEBUILDER_ASSETS environment variable:
```shell
KUBEBUILDER_ASSETS=$(pwd)/bin/k8s/$DIR_WITH_ASSETS
# where $DIR_WITH_ASSETS is the directory that contains binaries for your OS.
```
P.S. You can also integrate it within your IDE run configuration.

Then you can run the test:
```


You can also check the coverage:
```shell
task coverage
```
P.S. If you want to exclude some files from the coverage report, you can add them to the `.testcoverage.yml` file.



### Linting

```shell
task lint
```

### Subscriptions

To subscribe to events, you should use the SSE (Server-Sent Events) protocol.

Since GraphQL playground doesn't support it, you should use curl.

For instance, to subscribe to a change of a specific fields of the deployment, you can run the following command:
```shell
curl -H "Accept: text/event-stream" -H "Content-Type: application/json" http://localhost:3000/fullSchema/subscriptions \
-d '{"query": "subscription { apps_deployments(namespace: \"default\") { metadata { name } spec { replicas } } }"}'
```
Fields that will be listened are defined in the graphql query within the `{}` brackets.

If you want to listen to all fields, you can set `subscribeToAll` to `true`:
```shell
curl -H "Accept: text/event-stream" -H "Content-Type: application/json" http://localhost:3000/fullSchema/subscriptions \
-d '{"query": "subscription { apps_deployments(namespace: \"default\", subscribeToAll: true) { metadata { name } spec { replicas } } }"}'
```
If you want to listen to a specific deployment:
```shell
curl -H "Accept: text/event-stream" -H "Content-Type: application/json" http://localhost:3000/fullSchema/subscriptions \
-d '{"query": "subscription { apps_deployment(namespace: \"default\", name: \"my-new-deployment\") { metadata { name } spec { replicas } } }"}'
```

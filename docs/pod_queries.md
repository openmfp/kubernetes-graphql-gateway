# Pod Queries

You can use the following queries as a part of the [Quick Start](./quickstart.md) guide.

## Create a Pod:
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

## Get the Created Pod:
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

## Delete the Created Pod:
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
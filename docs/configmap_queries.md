# ConfigMap Queries

You can use the following queries as a part of [quickstart](./quickstart.md) guide.

## Create a ConfigMap:
```graphql
mutation {
  core {
    createConfigMap(
      namespace: "default",
      object: {
        metadata: {
          name: "example-config"
        },
        data: "key=val"
      }
    ) {
      metadata {
        name
      }
      data
    }
  }
}
```

## List ConfigMaps:
```graphql
{
  core {
    ConfigMaps {
      metadata {
        name
      }
      data
    }
  }
}
```

## Get a ConfigMap:
```graphql
{
  core {
    ConfigMap(name: "example-config", namespace: "default") {
      metadata {
        name
      }
      data
    }
  }
}
```

## Update a ConfigMap:
```graphql
mutation {
  core {
    updateConfigMap(
      name: "example-config",
      namespace: "default",
      object: {
        metadata: {
          labels: "hello=world"
        }
      }
    ) {
      metadata {
        name
        labels
      }
    }
  }
}
```

## Delete a ConfigMap:
```graphql
mutation {
  core {
    deleteConfigMap(
      name: "example-config", 
      namespace: "default"
    )
  }
}
```
# ConfigMap Queries

You can use the following queries as a part of [quickstart](./quickstart.md) guide.

## Create a ConfigMap:
```shell
mutation {
  core {
    createConfigMap(
      namespace: "default",
      object: {
        metadata: {
          name: "example-config"
        },
        data: { key: "val" }
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
```shell
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
```shell
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
```shell
mutation {
  core {
    updateConfigMap(
      name:"example-config"
      namespace: "default",
      object: {
        data: { key: "new-value" }
      }
    ) {
      metadata {
        name
        namespace
      }
      data
    }
  }
}
```

## Delete a ConfigMap:
```shell
mutation {
  core {
    deleteConfigMap(
      name: "example-config", 
      namespace: "default"
    )
  }
}
```
# Custom Queries

This page shows you examples queries and mutations for GraphQL to perform operations on the any resource in the Kuberenetes cluster. 
For questions on how to execute them, please find our [Quick Start Guide](./quickstart.md).

## typeByCategory

```shell
{
  typeByCategory(name:"all") {
    group
    version
    kind
    scope
  }
}
```

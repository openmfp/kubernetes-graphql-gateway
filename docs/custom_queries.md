# Custom Queries

You can use the following queries as a part of [Quick Start](./quickstart.md) guide.

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
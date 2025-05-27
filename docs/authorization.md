# Authorization

All requests must contain an `Authorization` header with a valid Bearer token by default:
```shell
{
    "Authorization": $YOUR_TOKEN
}
```
You can disable authorization by setting the following environment variable:
```shell
export LOCAL_DEVELOPMENT=true
```
This is useful for local development and testing purposes.

## Using Service Account Token

For local development you can:
1. Create a service account:
```shell
kubectl create sa app
```
2. Give it `admin` permissions.

⚠️ Warning: This gives all permissions across the entire cluster. Be careful.

```shell
kubectl create clusterrolebinding app2-cluster-admin \
  --clusterrole=cluster-admin \
  --serviceaccount=default:app
```
3. Generate token by running
```shell
kubectl create token app
```

To use the service account token directly, you must skip the user impersonation step:
```shell
export GATEWAY_SHOULD_IMPERSONATE=false
```

## Introspection authentication

By default, introspection requests (i.e. the requests that are made to fetch the GraphQL schema) are **not** protected by authorization.

You can protect those requests by setting the following environment variable:
```shell
export INTROSPECTION_AUTHENTICATION=true
```

### Error fetching schema

When GraphiQL page is loaded, it makes a request to fetch the GraphQL schema and there is no way to add the `Authorization` header to that request.

We have this [issue](https://github.com/openmfp/kubernetes-graphql-gateway/issues/217) open to fix this.

But for now, you can use the following workaround:
1. Open the GraphiQL page in your browser.
2. Add the `Authorization` header in the `Headers` section of the GraphiQL user interface like so:
3. Press `Re-fetch GraphQL schema` button in the left sidebar(third button from the top).
4. Now the GraphQL schema should be fetched, and you can use the GraphiQL interface as usual.

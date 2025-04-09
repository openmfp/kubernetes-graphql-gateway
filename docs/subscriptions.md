# Subscriptions

You can use the following queries as a part of [quickstart](./quickstart.md) guide.

To subscribe to events, you should use the SSE (Server-Sent Events) protocol.

Since GraphQL playground doesn't support it, you should use curl.

## Parameters
- `subscribeToAll`: if true, any field change will be sent to the client.
Otherwise, only fields defined within the `{}` brackets will be listened to.

Please note that only fields specified in `{}` brackets will be returned, even if `subscribeToAll: true`

## Prerequisites
```shell
GRAPHQL_URL=http://localhost:8080/root/graphql # update with your actual GraphQL endpoint
AUTHORIZATION_TOKEN=<your-token> # update this with your token, if LOCAL_DEVELOPMENT=false
```

### Subscribe to the ConfigMap resource

ConfigMap is present in both KCP and standard Kubernetes clusters, so we can use it right away without any additional setup.

After subscription, you can run mutations from [configmap queries](./configmap_queries.md) to see the changes in the subscription.

#### Subscribe to a change of a data field in all ConfigMaps:
```shell
curl \
  -H "Accept: text/event-stream" \
  -H "Content-Type: application/json" \
  -H "Authorization: $AUTHORIZATION_TOKEN" \
  -d '{"query": "subscription { core_configmaps { metadata { name } data }}"}' \
  $GRAPHQL_URL
```
#### Subscribe to a change of a data field in a specific ConfigMap:

```shell
curl \
  -H "Accept: text/event-stream" \
  -H "Content-Type: application/json" \
  -H "Authorization: $AUTHORIZATION_TOKEN" \
  -d '{"query": "subscription { core_configmap(name: \"example-config\", namespace: \"default\") { metadata { name } data }}"}' \
  $GRAPHQL_URL
```

#### Subscribe to a change of all fields in a specific ConfigMap:

Please note that only fields specified in `{}` brackets will be returned, even if `subscribeToAll: true`

```shell
curl \
  -H "Accept: text/event-stream" \
  -H "Content-Type: application/json" \
  -H "Authorization: $AUTHORIZATION_TOKEN" \
  -d '{"query": "subscription { core_configmap(name: \"example-config\", namespace: \"default\", subscribeToAll: true) { metadata { name } }}"}' \
  $GRAPHQL_URL
```

### Subscribe to the Account resource

If you have [Account](https://github.com/openmfp/account-operator/tree/main/config) CRD registered in your cluster, you can use the following queries:

After subscription, you can should run mutations against accounts to see the changes in the subscription.

#### Subscribe to a change of a displayName field in all accounts
```shell
curl \
  -H "Accept: text/event-stream" \
  -H "Content-Type: application/json" \
  -H "Authorization: $AUTHORIZATION_TOKEN" \
  -d '{"query": "subscription { core_openmfp_org_accounts { spec { displayName }}}"}' \
  $GRAPHQL_URL
```

#### Subscribe to a change of a displayName field in a specific account
```shell
curl \
  -H "Accept: text/event-stream" \
  -H "Content-Type: application/json" \
  -H "Authorization: $AUTHORIZATION_TOKEN" \
  -d '{"query": "subscription { core_openmfp_org_account(name: \"root-account\") { spec { displayName }}}"}' \
  $GRAPHQL_URL
```

#### Subscribe to a change of a displayName field in all accounts
```shell
curl \
  -H "Accept: text/event-stream" \
  -H "Content-Type: application/json" \
  -H "Authorization: $AUTHORIZATION_TOKEN" \
  -d '{"query": "subscription { core_openmfp_org_account(name: \"root-account\", subscribeToAll: true) { metadata { name } }}"}' \
  $GRAPHQL_URL
```
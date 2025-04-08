# Subscriptions

You can use the following queries as a part of [quickstart](./quickstart.md) guide.

To subscribe to events, you should use the SSE (Server-Sent Events) protocol.

Since GraphQL playground doesn't support it, you should use curl.

## Parameters
- `subscribeToAll`: if true, any field change will be sent to the client.
Otherwise, only fields defined within the {} brackets will be listened to.

Please note that only fields specified in `{}` brackets will be returned regardless `subscribeToAll: true`

## Examples

All queries below will work with KCP root workspace. 
In case you are using a different workspace, or a standard kubernetes cluster, you need to change the URL accordingly.

Also, if you run the Gateway with `LOCAL_DEVELOPMENT=false`, you need to add the `Authorization` header with the token.

### Subscribe to a change of a displayName field in a specific account
```shell
curl \
  -H "Accept: text/event-stream" \
  -H "Content-Type: application/json" \
  -d '{"query": "subscription { core_openmfp_org_account(name: \"root-account\") { spec { displayName }}}"}' \
  http://localhost:8080/root/graphql
```

### Listen to all fields of a specific account with subscribeToAll: true
```shell
curl \
  -H "Accept: text/event-stream" \
  -H "Content-Type: application/json" \
  -d '{"query": "subscription { core_openmfp_org_account(name: \"root-account\", subscribeToAll: true) { metadata { name } }}"}' \
  http://localhost:8080/root/graphql
```

### Subscribe to all accounts in the root workspace
```shell
curl \
  -H "Accept: text/event-stream" \
  -H "Content-Type: application/json" \
  -d '{"query": "subscription { core_openmfp_org_accounts { spec { displayName }}}"}' \
  http://localhost:8080/root/graphql
```
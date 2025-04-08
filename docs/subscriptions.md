# Subscriptions

You can use the following queries as a part of [quickstart](./quickstart.md) guide.

To subscribe to events, you should use the SSE (Server-Sent Events) protocol.

Since GraphQL playground doesn't support it, you should use curl.

All queries beneath will work with kcp root workspace. 
In case you are using a different workspace, or a standard kubernetes cluster, you need to change the URL accordingly.

Also, if you run gateway with `LOCAL_DEVELOPMENT=false`, you need to add the `Authorization` header with the token.

## Subscribe to a change of a displayName field in a specific account
```shell
curl \
  -H "Accept: text/event-stream" \
  -H "Content-Type: application/json" \
  -d '{"query": "subscription { core_openmfp_org_account(name: \"root-account\") { spec { displayName }}}"}' \
  http://localhost:8080/root/graphql
```
Fields that will be listened are defined in the graphql query within the `{}` brackets.

If you want to listen to all fields, you can set `subscribeToAll` to `true`:
```shell
curl \
  -H "Accept: text/event-stream" \
  -H "Content-Type: application/json" \
  -d '{"query": "subscription { core_openmfp_org_account(name: \"root-account\", subscribeToAll: true) { metadata { name } }}"}' \
  http://localhost:8080/root/graphql
```
P.S. Note, that only fields specified in `{}` brackets will be returned.

## Subscribe to all accounts in the root workspace
```shell
curl \
  -H "Accept: text/event-stream" \
  -H "Content-Type: application/json" \
  -d '{"query": "subscription { core_openmfp_org_accounts { spec { displayName }}}"}' \
  http://localhost:8080/root/graphql
```
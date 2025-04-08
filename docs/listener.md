# Listener

[back to the main readme](../README.md)

The Listener component is responsible for watching a Kubernetes cluster and generating OpenAPI specifications for the resources it finds. 

It stores these specifications in a directory, which can then be used by the [Gateway](./gateway.md) component to expose them as GraphQL endpoints.

For each workspace in KCP, the Listener will create a separate file in the specified directory. 

The Gateway will then watch this directory for changes and update the GraphQL schema accordingly.


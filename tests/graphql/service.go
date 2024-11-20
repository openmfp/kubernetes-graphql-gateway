package graphql

func CreateServiceMutation() string {
	return `
    mutation {
      core {
        createService(
          namespace: "default",
          object: {
            metadata: { name: "test-service" },
            spec: {
              selector: { app: "my-app" },
              ports: [
                {
                  protocol: "TCP",
                  port: 80,
                }
              ],
              type: "ClusterIP"
            }
          }
        ) {
          metadata {
            name
            namespace
          }
          spec {
            type
            clusterIP
            ports {
              port
            }
          }
        }
      }
    }
    `
}

func GetServiceQuery() string {
	return `
    query {
      core {
        Service(name: "test-service", namespace: "default") {
          metadata {
            name
            namespace
          }
          spec {
            type
            clusterIP
            ports {
              port
              targetPort
            }
          }
        }
      }
    }
    `
}

func DeleteServiceMutation() string {
	return `
    mutation {
      core {
        deleteService(name: "test-service", namespace: "default")
      }
    }
    `
}

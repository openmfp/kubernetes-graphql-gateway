# Dotted Keys Support

GraphQL doesn't support dots in field names, but Kubernetes uses dotted keys extensively (e.g., `app.kubernetes.io/name`). This document explains how to work with such fields.

## Supported Fields

The following fields support dotted keys using a `Label` array format:

- `metadata.labels`
- `metadata.annotations` 
- `spec.nodeSelector`
- `spec.selector.matchLabels`
- `spec.template.metadata.labels` (in Deployments)

## Querying

Use `key` and `value` sub-fields to access dotted keys:

```graphql
query {
  core {
    Pod(namespace: "default", name: "my-pod") {
      metadata {
        labels {
          key
          value
        }
        annotations {
          key  
          value
        }
      }
    }
  }
}
```

**Response:**
```json
{
  "data": {
    "core": {
      "Pod": {
        "metadata": {
          "labels": [
            {"key": "app.kubernetes.io/name", "value": "my-app"},
            {"key": "environment", "value": "production"}
          ],
          "annotations": [
            {"key": "deployment.kubernetes.io/revision", "value": "1"}
          ]
        }
      }
    }
  }
}
```

## Creating/Updating

Use array syntax with `key` and `value` objects:

```graphql
mutation {
  apps {
    createDeployment(
      namespace: "default"
      object: {
        metadata: {
          name: "my-app"
          labels: [
            {key: "app.kubernetes.io/name", value: "my-app"},
            {key: "app.kubernetes.io/version", value: "1.0.0"}
          ]
          annotations: [
            {key: "deployment.kubernetes.io/revision", value: "1"}
          ]
        }
        spec: {
          selector: {
            matchLabels: [
              {key: "app.kubernetes.io/name", value: "my-app"}
            ]
          }
          template: {
            spec: {
              nodeSelector: [
                {key: "kubernetes.io/arch", value: "amd64"}
              ]
            }
          }
        }
      }
    ) {
      metadata {
        name
      }
    }
  }
}
```

## Notes

- **No quotes** around `key` and `value` in GraphQL (they're field names, not strings)
- Arrays are automatically converted to Kubernetes `map[string]string` format
- Works with any keys containing dots or special characters
- Supports all standard Kubernetes label/annotation patterns 
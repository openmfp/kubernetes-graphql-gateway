# Virtual Workspaces

Virtual workspaces allow the listener to connect to external KCP workspaces or API exports without them being part of the main KCP cluster hierarchy. This enables accessing remote services and APIs through the GraphQL gateway.

## Configuration

Virtual workspaces are configured through a YAML configuration file that is mounted to the listener. The path to this file is specified using the `virtual-workspaces-config-path` configuration option.

### Configuration File Format

```yaml
virtualWorkspaces:
- name: example
  url: https://192.168.1.118:6443/services/apiexport/root/configmaps-view
- name: another-service
  url: https://your-kcp-server:6443/services/apiexport/root/your-export
```

### Configuration Options

- `virtualWorkspaces`: Array of virtual workspace definitions
  - `name`: Unique identifier for the virtual workspace (used in URL paths)
  - `url`: Full URL to the virtual workspace or API export

## Environment Variables

Set the configuration path using:

```bash
export VIRTUAL_WORKSPACES_CONFIG_PATH="/etc/config/virtual-workspaces.yaml"
```

Or use the default path: `/etc/config/virtual-workspaces.yaml`

## URL Pattern

Virtual workspaces are accessible through the gateway using the following URL pattern:

```
/kubernetes-graphql-gateway/virtualworkspace/{name}/query
```

For example:
- Normal workspace: `/kubernetes-graphql-gateway/root:abc:abc/query`
- Virtual workspace: `/kubernetes-graphql-gateway/virtualworkspace/example/query`

## How It Works

1. **Configuration Watching**: The listener watches the virtual workspaces configuration file for changes
2. **Schema Generation**: For each virtual workspace, the listener:
   - Creates a discovery client pointing to the virtual workspace URL
   - Generates OpenAPI schemas for the available resources
   - Stores the schema in a file at `virtualworkspace/{name}`
3. **Gateway Integration**: The gateway watches the schema files and exposes virtual workspaces as GraphQL endpoints

## File System Layout

Schema files for virtual workspaces are stored in the definitions directory with the following structure:

```
./bin/definitions/
├── root:workspace1:workspace2          # Regular KCP workspace
├── root:workspace3                     # Regular KCP workspace
└── virtualworkspace/
    ├── example                         # Virtual workspace schema
    └── another-service                 # Virtual workspace schema
```

## Example Usage

1. Create a configuration file:

```yaml
# /etc/config/virtual-workspaces.yaml
virtualWorkspaces:
- name: configmaps-view
  url: https://192.168.1.118:6443/services/apiexport/root/configmaps-view
```

2. Start the listener with the configuration:

```bash
export VIRTUAL_WORKSPACES_CONFIG_PATH="/etc/config/virtual-workspaces.yaml"
export KUBECONFIG=/path/to/your/kcp/admin.kubeconfig
go run main.go listener
```

3. The virtual workspace will be available at:
   - GraphQL endpoint: `/kubernetes-graphql-gateway/virtualworkspace/configmaps-view/query`

## Updating Configuration

The configuration file is watched for changes. When the file is modified:
- New virtual workspaces are automatically discovered and schema files generated
- Updated URLs trigger schema regeneration
- Removed virtual workspaces have their schema files deleted

## Troubleshooting

### Common Issues

1. **Invalid URL Format**: Ensure URLs are properly formatted and accessible
2. **Network Connectivity**: Verify the listener can reach the virtual workspace URLs
3. **Authentication**: Virtual workspaces use the same authentication as the base KCP connection

### Logs

Check listener logs for virtual workspace processing:

```bash
# Look for log entries with virtual workspace information
kubectl logs <listener-pod> | grep "virtual workspace"
``` 
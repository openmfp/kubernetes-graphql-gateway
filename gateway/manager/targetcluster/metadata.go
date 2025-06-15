package targetcluster

// ClusterMetadata represents cluster connection information embedded in schema files
// Simplified for standard Kubernetes clusters with kubeconfig authentication
type ClusterMetadata struct {
	Host string        `json:"host"`
	Path string        `json:"path"`
	Auth *AuthMetadata `json:"auth,omitempty"`
}

// AuthMetadata contains kubeconfig authentication for standard Kubernetes clusters
type AuthMetadata struct {
	Type       string `json:"type"`       // Only "kubeconfig" is supported
	Kubeconfig string `json:"kubeconfig"` // base64 encoded kubeconfig
}

// FileData represents the data extracted from a schema file
type FileData struct {
	Definitions map[string]interface{} `json:"definitions"`
	Metadata    *ClusterMetadata       `json:"x-cluster-metadata"`
}

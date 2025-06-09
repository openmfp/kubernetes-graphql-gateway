package targetcluster

// ClusterMetadata represents cluster connection information embedded in schema files
type ClusterMetadata struct {
	Host string        `json:"host"`
	Path string        `json:"path"`
	Auth *AuthMetadata `json:"auth,omitempty"`
	CA   *CAMetadata   `json:"ca,omitempty"`
}

// AuthMetadata contains authentication configuration for connecting to a cluster
type AuthMetadata struct {
	Type       string `json:"type"`
	Token      string `json:"token,omitempty"`      // base64 encoded
	CertData   string `json:"certData,omitempty"`   // base64 encoded
	KeyData    string `json:"keyData,omitempty"`    // base64 encoded
	Kubeconfig string `json:"kubeconfig,omitempty"` // base64 encoded
}

// CAMetadata contains certificate authority data for cluster connection
type CAMetadata struct {
	Data string `json:"data"` // base64 encoded
}

// FileData represents the data extracted from a schema file
type FileData struct {
	Definitions map[string]interface{} `json:"definitions"`
	Metadata    *ClusterMetadata       `json:"x-cluster-metadata"`
}

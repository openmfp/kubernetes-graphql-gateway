package gateway_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"k8s.io/client-go/rest"
)

const sleepTime = 500 * time.Millisecond

type core struct {
	Pod       *podData `json:"Pod,omitempty"`
	CreatePod *podData `json:"createPod,omitempty"`
}

type metadata struct {
	Name      string
	Namespace string
}

type GraphQLResponse struct {
	Data   *graphQLData   `json:"data,omitempty"`
	Errors []graphQLError `json:"errors,omitempty"`
}

type graphQLData struct {
	Core                   *core                   `json:"core,omitempty"`
	CoreOpenmfpOrg         *coreOpenmfpOrg         `json:"core_openmfp_org,omitempty"`
	RbacAuthorizationK8sIO *RbacAuthorizationK8sIO `json:"rbac_authorization_k8s_io,omitempty"`
}

type graphQLError struct {
	Message   string                 `json:"message"`
	Locations []GraphQLErrorLocation `json:"locations,omitempty"`
	Path      []interface{}          `json:"path,omitempty"`
}

type GraphQLErrorLocation struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

func sendRequest(url, query string) (*GraphQLResponse, int, error) {
	reqBody := map[string]string{
		"query": query,
	}
	reqBodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, 0, err
	}

	resp, err := http.Post(url, "application/json", bytes.NewReader(reqBodyBytes))
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, err
	}

	var bodyResp GraphQLResponse
	err = json.Unmarshal(respBytes, &bodyResp)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("response body is not json, but %s", respBytes)
	}

	return &bodyResp, resp.StatusCode, err
}

// writeToFile adds a new file to the watched directory which will trigger schema generation
func writeToFile(from, to string) error {
	specContent, err := os.ReadFile(from)
	if err != nil {
		return err
	}

	err = os.WriteFile(to, specContent, 0644)
	if err != nil {
		return err
	}

	// let's give some time to the manager to process the file and create a url
	time.Sleep(sleepTime)

	return nil
}

// createTestSchemaFile creates a proper schema file with cluster metadata pointing to the test environment
func createTestSchemaFile(restConfig *rest.Config, token, filePath string) error {
	// Read the base Kubernetes schema definitions
	specContent, err := os.ReadFile(filepath.Join("testdata", "kubernetes"))
	if err != nil {
		return fmt.Errorf("failed to read kubernetes schema: %w", err)
	}

	// Parse the existing schema to extract definitions
	var schemaData map[string]interface{}
	if err := json.Unmarshal(specContent, &schemaData); err != nil {
		return fmt.Errorf("failed to parse schema JSON: %w", err)
	}

	// Use the BearerToken from restConfig if available, otherwise fall back to provided token
	authToken := token
	if restConfig.BearerToken != "" {
		authToken = restConfig.BearerToken
	}

	// Add cluster metadata for connecting to the test environment
	// Encode the token as base64 since that's what the gateway expects
	encodedToken := base64.StdEncoding.EncodeToString([]byte(authToken))

	schemaData["x-cluster-metadata"] = map[string]interface{}{
		"host": restConfig.Host,
		"path": "myWorkspace", // This should match the workspace name used in tests
		"auth": map[string]interface{}{
			"type":  "token",
			"token": encodedToken,
		},
	}

	// If the rest config has CA data, include it
	if len(restConfig.CAData) > 0 {
		encodedCA := base64.StdEncoding.EncodeToString(restConfig.CAData)
		schemaData["x-cluster-metadata"].(map[string]interface{})["ca"] = map[string]interface{}{
			"data": encodedCA,
		}
	}

	// Write the modified schema file
	updatedContent, err := json.Marshal(schemaData)
	if err != nil {
		return fmt.Errorf("failed to marshal updated schema: %w", err)
	}

	if err := os.WriteFile(filePath, updatedContent, 0644); err != nil {
		return fmt.Errorf("failed to write schema file: %w", err)
	}

	// let's give some time to the manager to process the file and create a url
	time.Sleep(sleepTime)

	return nil
}

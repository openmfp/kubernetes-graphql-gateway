package helpers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const SleepTime = 2000 * time.Millisecond

type Core struct {
	Pod       *PodData `json:"Pod,omitempty"`
	CreatePod *PodData `json:"createPod,omitempty"`
}

type Metadata struct {
	Name      string
	Namespace string
}

type GraphQLResponse struct {
	Data   *GraphQLData   `json:"data,omitempty"`
	Errors []GraphQLError `json:"errors,omitempty"`
}

type GraphQLData struct {
	Core                   *Core                   `json:"core,omitempty"`
	CoreOpenmfpIO          *CoreOpenmfpIo          `json:"core_openmfp_io,omitempty"`
	RbacAuthorizationK8sIo *RbacAuthorizationK8sIo `json:"rbac_authorization_k8s_io,omitempty"`
}

type GraphQLError struct {
	Message   string                 `json:"message"`
	Locations []GraphQLErrorLocation `json:"locations,omitempty"`
	Path      []interface{}          `json:"path,omitempty"`
}

type GraphQLErrorLocation struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

func SendRequest(url, query string) (*GraphQLResponse, int, error) {
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

// WriteToFile adds a new file to the watched directory which will trigger schema generation
func WriteToFile(from, to string) error {
	specContent, err := os.ReadFile(from)
	if err != nil {
		return err
	}

	err = os.WriteFile(to, specContent, 0644)
	if err != nil {
		return err
	}

	// let's give some time to the manager to process the file and create a url
	time.Sleep(SleepTime)

	return nil
}

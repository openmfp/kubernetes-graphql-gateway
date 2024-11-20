package graphql

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
)

type GraphQLResponse struct {
	Data   *GraphQLData   `json:"data,omitempty"`
	Errors []GraphQLError `json:"errors,omitempty"`
}

type GraphQLData struct {
	Core *CoreData `json:"core,omitempty"`
}

type CoreData struct {
	Pod           *PodData     `json:"Pod,omitempty"`
	Service       *ServiceData `json:"Service,omitempty"`
	CreatePod     *PodData     `json:"createPod,omitempty"`
	CreateService *ServiceData `json:"createService,omitempty"`
	DeleteService *bool        `json:"deleteService,omitempty"`
}

type PodData struct {
	Metadata Metadata `json:"metadata"`
	Spec     PodSpec  `json:"spec"`
}

type ServiceData struct {
	Metadata Metadata       `json:"metadata"`
	Spec     ServiceSpec    `json:"spec"`
	Status   *ServiceStatus `json:"status,omitempty"`
}

type Metadata struct {
	Name      string
	Namespace string
}

type PodSpec struct {
	Containers []Container `json:"containers"`
}

type Container struct {
	Name  string `json:"name"`
	Image string `json:"image"`
}

type ServiceSpec struct {
	Type  string        `json:"type"`
	Ports []ServicePort `json:"ports"`
}

type ServicePort struct {
	Port int `json:"port"`
}

type ServiceStatus struct{}

type GraphQLError struct {
	Message   string                 `json:"message"`
	Locations []GraphQLErrorLocation `json:"locations,omitempty"`
	Path      []interface{}          `json:"path,omitempty"`
}

type GraphQLErrorLocation struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

func SendRequest(url, query string) ([]byte, int, error) {
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

	return respBytes, resp.StatusCode, nil
}

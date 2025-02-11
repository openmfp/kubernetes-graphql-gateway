package apischema

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"k8s.io/client-go/discovery"
	"k8s.io/client-go/openapi"
	"k8s.io/kube-openapi/pkg/validation/spec"
)

var (
	errInvalidPath  = errors.New("path doesn't contain the / separator")
	errNotPreferred = errors.New("path ApiGroup does not belong to the server preferred APIs")
)

func getSchemaForPath(preferredApiGroups []string, path string, gv openapi.GroupVersion) (map[string]*spec.Schema, error) {
	if !strings.Contains(path, separator) {
		return nil, errInvalidPath
	}
	pathApiGroupArray := strings.Split(path, separator)
	pathApiGroup := strings.Join(pathApiGroupArray[1:], separator)
	// filer out apiGroups that aren't in the preferred list
	if !slices.Contains(preferredApiGroups, pathApiGroup) {
		return nil, errNotPreferred
	}

	b, err := gv.Schema(discovery.AcceptV1)
	if err != nil {
		return nil, fmt.Errorf("failed to get schema for path %s :%w", path, err)
	}

	resp := &schemaResponse{}
	if err := json.Unmarshal(b, resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal schema for path %s :%w", path, err)
	}
	return resp.Components.Schemas, nil
}

func resolveForPaths(oc openapi.Client, preferredApiGroups []string) ([]byte, error) {
	apiv3Paths, err := oc.Paths()
	if err != nil {
		return nil, fmt.Errorf("failed to get OpenAPI paths: %w", err)
	}

	schemas := make(map[string]*spec.Schema)
	for path, gv := range apiv3Paths {
		schema, err := getSchemaForPath(preferredApiGroups, path, gv)
		if err != nil {
			//TODO: debug log?
			continue
		}
		maps.Copy(schemas, schema)
	}
	v3JSON, err := json.Marshal(&schemaResponse{
		Components: schemasComponentsWrapper{
			Schemas: schemas,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal openAPI v3 schema: %w", err)
	}
	v2JSON, err := ConvertJSON(v3JSON)
	if err != nil {
		return nil, fmt.Errorf("failed to convert openAPI v3 schema to v2: %w", err)
	}

	return v2JSON, nil
}

func resolveSchema(dc discovery.DiscoveryInterface) ([]byte, error) {
	preferredApiGroups := []string{}
	apiResList, err := dc.ServerPreferredResources()
	if err != nil {
		return nil, fmt.Errorf("failed to get server preferred resources: %w", err)
	}
	for _, apiRes := range apiResList {
		preferredApiGroups = append(preferredApiGroups, apiRes.GroupVersion)
	}

	return resolveForPaths(dc.OpenAPIV3(), preferredApiGroups)
}

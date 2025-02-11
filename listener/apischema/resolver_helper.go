package apischema

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"k8s.io/client-go/discovery"
	"k8s.io/client-go/openapi"
	"k8s.io/kube-openapi/pkg/validation/spec"
)

var (
	errInvalidKey   = errors.New("key path doesn't contain the / separator")
	errNotPreferred = errors.New("key path ApiGroup does not belong to the server preferred APIs")
)

func getSchemaForPath(preferredApiGroups []string, path string, gv openapi.GroupVersion) (map[string]*spec.Schema, error) {
	if !strings.Contains(path, separator) {
		return nil, errInvalidKey
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

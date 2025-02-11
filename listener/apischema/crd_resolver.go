package apischema

import (
	"errors"
	"fmt"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
)

type CRDResolver struct {
	*discovery.DiscoveryClient
}

func (cr *CRDResolver) ResolveApiSchema(crd *apiextensionsv1.CustomResourceDefinition) ([]byte, error) {
	gvk, err := getCRDGroupVersionKind(crd.Spec)
	if err != nil {
		return nil, fmt.Errorf("failed to get CRD GVK: %w", err)
	}
	preferredApiGroups := []string{}
	apiResLists, err := cr.ServerPreferredResources()
	if err != nil {
		return nil, fmt.Errorf("failed to get server preferred resources: %w", err)
	}
	isCRDFound := false
	for _, apiResources := range apiResLists {
		gv := apiResources.GroupVersion
		if gv == fmt.Sprintf("%s/%s", gvk.Group, gvk.Version) {
			for _, res := range apiResources.APIResources {
				if res.Kind == gvk.Kind {
					isCRDFound = true
					break
				}
			}
		}
		preferredApiGroups = append(preferredApiGroups, gv)
	}

	if !isCRDFound {
		return nil, errors.New("failed to find CRD GVK in API preferred resources")
	}

	return resolveForPaths(cr.OpenAPIV3(), preferredApiGroups)
}

func (cr *CRDResolver) Resolve() ([]byte, error) {
	return resolveSchema(cr.DiscoveryClient)
}

func getCRDGroupVersionKind(spec apiextensionsv1.CustomResourceDefinitionSpec) (*metav1.GroupVersionKind, error) {
	for _, v := range spec.Versions {
		if v.Storage {
			return &metav1.GroupVersionKind{
				Group:   spec.Group,
				Version: v.Name,
				Kind:    spec.Names.Kind,
			}, nil
		}
	}
	return nil, errors.New("failed to find storage version")
}

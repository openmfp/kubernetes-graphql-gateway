package controller

import (
	"bytes"
	"fmt"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

func (r *CRDReconciler) updateAPISchema() error {
	savedJSON, err := r.io.Read(r.ClusterName)
	if err != nil {
		return fmt.Errorf("failed to read JSON from filesystem: %w", err)
	}
	actualJSON, err := r.Resolve()
	if err != nil {
		return fmt.Errorf("failed to resolve server JSON schema: %w", err)
	}
	if !bytes.Equal(actualJSON, savedJSON) {
		if err := r.io.Write(actualJSON, r.ClusterName); err != nil {
			return fmt.Errorf("failed to write JSON to filesystem: %w", err)
		}
	}
	return nil
}

func (r *CRDReconciler) updateAPISchemaWith(crd *apiextensionsv1.CustomResourceDefinition) error {
	savedJSON, err := r.io.Read(r.ClusterName)
	if err != nil {
		return fmt.Errorf("failed to read JSON from filesystem: %w", err)
	}
	actualJSON, err := r.ResolveApiSchema(crd)
	if err != nil {
		return fmt.Errorf("failed to resolve server JSON schema: %w", err)
	}
	if !bytes.Equal(actualJSON, savedJSON) {
		if err := r.io.Write(actualJSON, r.ClusterName); err != nil {
			return fmt.Errorf("failed to write JSON to filesystem: %w", err)
		}
	}
	return nil
}

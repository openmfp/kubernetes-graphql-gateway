package singlecluster

import (
	"context"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/openmfp/golang-commons/controller/lifecycle"
	commonserrors "github.com/openmfp/golang-commons/errors"
)

// generateSchemaSubroutine handles schema generation for the standard Kubernetes cluster
type generateSchemaSubroutine struct {
	reconciler *StandardReconciler
}

func (s *generateSchemaSubroutine) Process(ctx context.Context, instance lifecycle.RuntimeObject) (ctrl.Result, commonserrors.OperatorError) {
	s.reconciler.log.Info().Msg("generating schema for standard Kubernetes cluster")

	// Generate schema using the discovery client and REST mapper
	schemaJSON, err := s.reconciler.schemaResolver.Resolve(s.reconciler.discoveryClient, s.reconciler.restMapper)
	if err != nil {
		s.reconciler.log.Error().Err(err).Msg("failed to resolve schema")
		return ctrl.Result{}, commonserrors.NewOperatorError(err, false, false)
	}

	// Write schema to file using the kubernetes cluster name
	if err := s.reconciler.ioHandler.Write(schemaJSON, kubernetesClusterName); err != nil {
		s.reconciler.log.Error().Err(err).Msg("failed to write schema")
		return ctrl.Result{}, commonserrors.NewOperatorError(err, false, false)
	}

	s.reconciler.log.Info().Str("filename", kubernetesClusterName).Msg("successfully generated schema")
	return ctrl.Result{}, nil
}

func (s *generateSchemaSubroutine) Finalize(ctx context.Context, instance lifecycle.RuntimeObject) (ctrl.Result, commonserrors.OperatorError) {
	return ctrl.Result{}, nil
}

func (s *generateSchemaSubroutine) GetName() string {
	return "generate-schema"
}

func (s *generateSchemaSubroutine) Finalizers() []string {
	return nil
}

// processClusterAccessSubroutine handles processing of ClusterAccess resources if they exist
type processClusterAccessSubroutine struct {
	reconciler *StandardReconciler
}

func (s *processClusterAccessSubroutine) Process(ctx context.Context, instance lifecycle.RuntimeObject) (ctrl.Result, commonserrors.OperatorError) {
	// Check if we have a CRD resource to process
	if instance != nil {
		crd, ok := instance.(*apiextensionsv1.CustomResourceDefinition)
		if ok {
			s.reconciler.log.Info().Str("crd", crd.GetName()).Msg("processing CRD resource")
			// Additional CRD-specific processing could go here
		}
	}

	s.reconciler.log.Info().Msg("completed processing ClusterAccess resources")
	return ctrl.Result{}, nil
}

func (s *processClusterAccessSubroutine) Finalize(ctx context.Context, instance lifecycle.RuntimeObject) (ctrl.Result, commonserrors.OperatorError) {
	return ctrl.Result{}, nil
}

func (s *processClusterAccessSubroutine) GetName() string {
	return "process-cluster-access"
}

func (s *processClusterAccessSubroutine) Finalizers() []string {
	return nil
}

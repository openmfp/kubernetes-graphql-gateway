package kcp

import (
	"context"
	"errors"

	ctrl "sigs.k8s.io/controller-runtime"

	kcpapis "github.com/kcp-dev/kcp/sdk/apis/apis/v1alpha1"
	"github.com/openmfp/golang-commons/controller/lifecycle"
	commonserrors "github.com/openmfp/golang-commons/errors"
)

// processAPIBindingSubroutine handles processing of APIBinding resources in KCP
type processAPIBindingSubroutine struct {
	reconciler *KCPReconciler
}

func (s *processAPIBindingSubroutine) Process(ctx context.Context, instance lifecycle.RuntimeObject) (ctrl.Result, commonserrors.OperatorError) {
	apiBinding, ok := instance.(*kcpapis.APIBinding)
	if !ok {
		s.reconciler.log.Error().Msg("instance is not an APIBinding resource")
		return ctrl.Result{}, commonserrors.NewOperatorError(errors.New("invalid resource type"), false, false)
	}

	apiBindingName := apiBinding.GetName()
	s.reconciler.log.Info().Str("apiBinding", apiBindingName).Msg("processing APIBinding resource")

	// For KCP, we typically want to generate schema for the root workspace
	// Try using "root" as the workspace name first
	workspaceName := "root"

	s.reconciler.log.Info().Str("apiBinding", apiBindingName).Str("workspaceName", workspaceName).Msg("processing workspace")

	// Create discovery client for this workspace
	discoveryClient, err := s.reconciler.discoveryFactory.ClientForCluster(workspaceName)
	if err != nil {
		s.reconciler.log.Error().Err(err).Str("apiBinding", apiBindingName).Str("workspaceName", workspaceName).Msg("failed to create discovery client")
		// If root fails, try with the APIBinding name
		workspaceName = apiBindingName
		s.reconciler.log.Info().Str("apiBinding", apiBindingName).Str("workspaceName", workspaceName).Msg("retrying with APIBinding name")

		discoveryClient, err = s.reconciler.discoveryFactory.ClientForCluster(workspaceName)
		if err != nil {
			s.reconciler.log.Error().Err(err).Str("apiBinding", apiBindingName).Str("workspaceName", workspaceName).Msg("failed to create discovery client with APIBinding name")
			return ctrl.Result{}, commonserrors.NewOperatorError(err, false, false)
		}
	}

	// Create REST mapper for this workspace
	restMapper, err := s.reconciler.discoveryFactory.RestMapperForCluster(workspaceName)
	if err != nil {
		s.reconciler.log.Error().Err(err).Str("apiBinding", apiBindingName).Str("workspaceName", workspaceName).Msg("failed to create REST mapper")
		return ctrl.Result{}, commonserrors.NewOperatorError(err, false, false)
	}

	// Generate schema for this workspace
	schemaJSON, err := s.reconciler.schemaResolver.Resolve(discoveryClient, restMapper)
	if err != nil {
		s.reconciler.log.Error().Err(err).Str("apiBinding", apiBindingName).Str("workspaceName", workspaceName).Msg("failed to resolve schema")
		return ctrl.Result{}, commonserrors.NewOperatorError(err, false, false)
	}

	// For root workspace, always use "root" as filename
	filename := "root"
	if workspaceName != "root" {
		// For other workspaces, use cleaned workspace name
		filename = s.cleanWorkspaceNameForFilename(workspaceName)
		if filename == "" {
			filename = apiBindingName
		}
	}

	// Write schema to file
	if err := s.reconciler.ioHandler.Write(schemaJSON, filename); err != nil {
		s.reconciler.log.Error().Err(err).Str("apiBinding", apiBindingName).Str("workspaceName", workspaceName).Str("filename", filename).Msg("failed to write schema")
		return ctrl.Result{}, commonserrors.NewOperatorError(err, false, false)
	}

	s.reconciler.log.Info().Str("apiBinding", apiBindingName).Str("workspaceName", workspaceName).Str("filename", filename).Msg("successfully generated schema for workspace")
	return ctrl.Result{}, nil
}

// cleanWorkspaceNameForFilename converts workspace name to a safe filename
func (s *processAPIBindingSubroutine) cleanWorkspaceNameForFilename(workspaceName string) string {
	if workspaceName == "" {
		return ""
	}

	// Simple cleaning: replace characters that are problematic for filenames
	filename := workspaceName
	filename = replaceChar(filename, ":", "-")
	filename = replaceChar(filename, "/", "-")
	filename = replaceChar(filename, "\\", "-")
	filename = replaceChar(filename, " ", "-")

	// Handle special case for root workspace
	if filename == "-root" || filename == "root" {
		return "root"
	}

	return filename
}

// replaceChar is a simple string replacement helper
func replaceChar(s, old, new string) string {
	result := ""
	for _, char := range s {
		if string(char) == old {
			result += new
		} else {
			result += string(char)
		}
	}
	return result
}

func (s *processAPIBindingSubroutine) Finalize(ctx context.Context, instance lifecycle.RuntimeObject) (ctrl.Result, commonserrors.OperatorError) {
	return ctrl.Result{}, nil
}

func (s *processAPIBindingSubroutine) GetName() string {
	return "process-api-binding"
}

func (s *processAPIBindingSubroutine) Finalizers() []string {
	return nil
}

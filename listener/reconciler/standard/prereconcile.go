package standard

import (
	"bytes"
	"errors"
	"io/fs"

	"github.com/openmfp/kubernetes-graphql-gateway/listener/apischema"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/workspacefile"
)

var (
	ErrResolveSchema = errors.New("failed to resolve server JSON schema")
	ErrReadJSON      = errors.New("failed to read JSON from filesystem")
	ErrWriteJSON     = errors.New("failed to write JSON to filesystem")
)

// preReconcile generates schema directly from the current cluster (original main branch approach)
func preReconcile(
	cr *apischema.CRDResolver,
	io workspacefile.IOHandler,
) error {
	actualJSON, err := cr.Resolve()
	if err != nil {
		return errors.Join(ErrResolveSchema, err)
	}

	savedJSON, err := io.Read(kubernetesClusterName)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return io.Write(actualJSON, kubernetesClusterName)
		}
		return errors.Join(ErrReadJSON, err)
	}

	if !bytes.Equal(actualJSON, savedJSON) {
		if err := io.Write(actualJSON, kubernetesClusterName); err != nil {
			return errors.Join(ErrWriteJSON, err)
		}
	}

	return nil
}

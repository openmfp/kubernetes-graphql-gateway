package workspacefile

import (
	"os"
	"path"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

var testJSON = []byte("{\"key\":\"value\"}")

func TestNewIOHandler(t *testing.T) {
	tempDir := t.TempDir()

	tests := map[string]struct {
		schemasDir string
		expectErr  bool
	}{
		"Valid directory":        {schemasDir: tempDir, expectErr: false},
		"Non-existent directory": {schemasDir: path.Join(tempDir, "non-existent"), expectErr: true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := NewIOHandler(tc.schemasDir)
			if tc.expectErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestRead(t *testing.T) {
	tempDir := t.TempDir()

	validClusterName := "root:sap:openmfp"

	validFile := filepath.Join(tempDir, validClusterName)

	os.WriteFile(validFile, testJSON, 0644)

	handler := &IOHandler{
		schemasDir: tempDir,
	}

	tests := map[string]struct {
		clusterName string
		expectErr   bool
	}{
		"Valid file":        {clusterName: validClusterName, expectErr: false},
		"Non-existent file": {clusterName: "root:non-existent", expectErr: true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := handler.Read(tc.clusterName)
			if tc.expectErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestWrite(t *testing.T) {
	tempDir := t.TempDir()
	handler := &IOHandler{
		schemasDir: tempDir,
	}

	tests := map[string]struct {
		clusterName string
		expectErr   bool
	}{
		"Valid write":  {clusterName: "root:sap:openmfp", expectErr: false},
		"Invalid path": {clusterName: "invalid/root:invalid", expectErr: true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if err := handler.Write(testJSON, tc.clusterName); tc.expectErr {
				assert.Error(t, err)
				return
			}

			writtenData, err := os.ReadFile(filepath.Join(tempDir, tc.clusterName))
			assert.NoError(t, err)
			assert.Equal(t, string(writtenData), string(testJSON))
		})
	}
}

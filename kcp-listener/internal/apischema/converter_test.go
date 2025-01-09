package apischema_test

import (
	"encoding/json"
	"os"
	"path"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/getkin/kin-openapi/openapi2"
	openapi_v2 "github.com/google/gnostic-models/openapiv2"
	"github.com/openmfp/crd-gql-gateway/kcp-listener/internal/apischema"
	"github.com/stretchr/testify/assert"
)

const (
	gnosticDocPath = "./testdata/gnosticObjectMeta.json"
	kinDocPath     = "./testdata/kinObjectMeta.json"
	testDataDir    = "./testdata"
)

func TestConvert(t *testing.T) {
	inJSON, inErr := os.ReadFile(kinDocPath)
	assert.NoError(t, inErr)
	assert.NotNil(t, inJSON)
	kinDoc := &openapi2.T{}
	kinErr := json.Unmarshal(inJSON, kinDoc)
	assert.NoError(t, kinErr)
	assert.NotEmpty(t, kinDoc)
	gnosticDoc, cErr := apischema.ConvertToGnosticDocument(kinDoc)
	assert.NoError(t, cErr)
	assert.NotEmpty(t, gnosticDoc)
	gnosticJSON, mErr := json.Marshal(gnosticDoc)
	assert.NoError(t, mErr)
	assert.NotEmpty(t, gnosticJSON)
	//gnosticDoc.YAMLValue()
	// gnosticYaml, yErr := yamlValue(gnosticDoc)
	// assert.NoError(t, yErr)
	// assert.NotEmpty(t, gnosticYaml)
	// gnosticJSON, ytjErr := k8syaml.YAMLToJSON(gnosticYaml)
	// assert.NoError(t, ytjErr)
	// assert.NotEmpty(t, gnosticJSON)
	wErr := os.WriteFile(path.Join(testDataDir, "gnosticOut.json"), gnosticJSON, os.ModePerm)
	assert.NoError(t, wErr)
	// outJSON, outErr := os.ReadFile(gnosticDocPath)
	// assert.NoError(t, outErr)
	// assert.NotNil(t, outJSON)
	// expectedGnosticDoc, pErr := openapi_v2.ParseDocument(outJSON)
	// assert.NoError(t, pErr)
	// assert.NotNil(t, expectedGnosticDoc)
	// assert.Equal(t, expectedGnosticDoc, gnosticDoc)
	// assert.Equal(t, outJSON, gnosticJSON)

}

// YAMLValue produces a serialized YAML representation of the document.
func YAMLValue(d *openapi_v2.Document) ([]byte, error) {
	rawInfo := d.ToRawInfo()
	rawInfo = &yaml.Node{
		Kind:    yaml.DocumentNode,
		Content: []*yaml.Node{rawInfo},
	}
	return yaml.Marshal(rawInfo)
}

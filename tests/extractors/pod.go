package extractors

import "fmt"

type PodData struct {
	Metadata Metadata
	Spec     Spec
}

type Metadata struct {
	Name      string
	Namespace string
}

type Spec struct {
	Containers []Container
}

type Container struct {
	Name  string
	Image string
}

func ExtractPodData(response map[string]interface{}) (*PodData, error) {
	data, ok := response["data"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("response data not found")
	}
	coreData, ok := data["core"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("core data not found")
	}
	podData, ok := coreData["Pod"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("Pod data not found")
	}
	metadata, ok := podData["metadata"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("metadata not found")
	}
	spec, ok := podData["spec"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("spec not found")
	}
	containersInterface, ok := spec["containers"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("containers not found")
	}
	containers := make([]Container, len(containersInterface))
	for i, c := range containersInterface {
		containerMap, ok := c.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("container data invalid")
		}
		name, _ := containerMap["name"].(string)
		image, _ := containerMap["image"].(string)
		containers[i] = Container{
			Name:  name,
			Image: image,
		}
	}
	pod := &PodData{
		Metadata: Metadata{
			Name:      metadata["name"].(string),
			Namespace: metadata["namespace"].(string),
		},
		Spec: Spec{
			Containers: containers,
		},
	}
	return pod, nil
}

package extractors

import "fmt"

type ServiceData struct {
	Metadata Metadata
	Spec     ServiceSpec
}

type ServiceSpec struct {
	Type  string
	Ports []ServicePort
}

type ServicePort struct {
	Port       int
	TargetPort int
}

func ExtractServiceData(response map[string]interface{}) (*ServiceData, error) {
	data, ok := response["data"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("response data not found")
	}
	coreData, ok := data["core"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("core data not found")
	}
	serviceData, ok := coreData["Service"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("Service data not found")
	}
	metadata, ok := serviceData["metadata"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("metadata not found")
	}
	spec, ok := serviceData["spec"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("spec not found")
	}
	portsInterface, ok := spec["ports"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("ports not found")
	}
	ports := make([]ServicePort, len(portsInterface))
	for i, p := range portsInterface {
		portMap, ok := p.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("port data invalid")
		}
		portValue, _ := portMap["port"].(float64)
		targetPortValue, _ := portMap["targetPort"].(float64)
		ports[i] = ServicePort{
			Port:       int(portValue),
			TargetPort: int(targetPortValue),
		}
	}
	service := &ServiceData{
		Metadata: Metadata{
			Name:      metadata["name"].(string),
			Namespace: metadata["namespace"].(string),
		},
		Spec: ServiceSpec{
			Type:  spec["type"].(string),
			Ports: ports,
		},
	}
	return service, nil
}

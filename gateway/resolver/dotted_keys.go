package resolver

// graphqlToKubernetes converts GraphQL input format to Kubernetes API format
// []Label → map[string]string (for CREATE/UPDATE operations)
func graphqlToKubernetes(obj any) any {
	objMap, ok := obj.(map[string]any)
	if !ok {
		return obj
	}

	// Process metadata.labels and metadata.annotations
	if metadata := objMap["metadata"]; metadata != nil {
		objMap["metadata"] = processMetadataToMaps(metadata)
	}

	// Process spec fields
	if spec := objMap["spec"]; spec != nil {
		objMap["spec"] = processSpecToMaps(spec)
	}

	return obj
}

// kubernetesToGraphQL converts Kubernetes API format to GraphQL output format
// map[string]string → []Label (for QUERY operations)
func kubernetesToGraphQL(obj any) any {
	objMap, ok := obj.(map[string]any)
	if !ok {
		return obj
	}

	// Process metadata.labels and metadata.annotations
	if metadata := objMap["metadata"]; metadata != nil {
		objMap["metadata"] = processMetadataToArrays(metadata)
	}

	// Process spec fields
	if spec := objMap["spec"]; spec != nil {
		objMap["spec"] = processSpecToArrays(spec)
	}

	return obj
}

// processMetadataToArrays handles metadata field conversion to arrays
func processMetadataToArrays(metadata any) any {
	metadataMap, ok := metadata.(map[string]any)
	if !ok {
		return metadata
	}

	for k, v := range metadataMap {
		if (k == "labels" || k == "annotations") && v != nil {
			metadataMap[k] = mapToArray(v)
		}
	}
	return metadata
}

// processMetadataToMaps handles metadata field conversion to maps
func processMetadataToMaps(metadata any) any {
	metadataMap, ok := metadata.(map[string]any)
	if !ok {
		return metadata
	}

	for k, v := range metadataMap {
		if (k == "labels" || k == "annotations") && v != nil {
			metadataMap[k] = arrayToMap(v)
		}
	}
	return metadata
}

// processSpecToArrays handles spec field conversion to arrays
func processSpecToArrays(spec any) any {
	specMap, ok := spec.(map[string]any)
	if !ok {
		return spec
	}

	for k, v := range specMap {
		if k == "nodeSelector" && v != nil {
			specMap[k] = mapToArray(v)
		} else if k == "selector" && v != nil {
			specMap[k] = processSelectorToArrays(v)
		} else if k == "template" && v != nil {
			specMap[k] = processTemplateToArrays(v)
		}
	}
	return spec
}

// processSpecToMaps handles spec field conversion to maps
func processSpecToMaps(spec any) any {
	specMap, ok := spec.(map[string]any)
	if !ok {
		return spec
	}

	for k, v := range specMap {
		if k == "nodeSelector" && v != nil {
			specMap[k] = arrayToMap(v)
		} else if k == "selector" && v != nil {
			specMap[k] = processSelectorToMaps(v)
		} else if k == "template" && v != nil {
			specMap[k] = processTemplateToMaps(v)
		}
	}
	return spec
}

// processSelectorToArrays handles spec.selector.matchLabels conversion to arrays
func processSelectorToArrays(selector any) any {
	selectorMap, ok := selector.(map[string]any)
	if !ok {
		return selector
	}

	for k, v := range selectorMap {
		if k == "matchLabels" && v != nil {
			selectorMap[k] = mapToArray(v)
		}
	}
	return selector
}

// processSelectorToMaps handles spec.selector.matchLabels conversion to maps
func processSelectorToMaps(selector any) any {
	selectorMap, ok := selector.(map[string]any)
	if !ok {
		return selector
	}

	for k, v := range selectorMap {
		if k == "matchLabels" && v != nil {
			selectorMap[k] = arrayToMap(v)
		}
	}
	return selector
}

// processTemplateToArrays handles spec.template.metadata and spec.template.spec conversion to arrays
func processTemplateToArrays(template any) any {
	templateMap, ok := template.(map[string]any)
	if !ok {
		return template
	}

	for k, v := range templateMap {
		if k == "metadata" && v != nil {
			templateMap[k] = processMetadataToArrays(v)
		} else if k == "spec" && v != nil {
			templateMap[k] = processSpecToArrays(v)
		}
	}
	return template
}

// processTemplateToMaps handles spec.template.metadata and spec.template.spec conversion to maps
func processTemplateToMaps(template any) any {
	templateMap, ok := template.(map[string]any)
	if !ok {
		return template
	}

	for k, v := range templateMap {
		if k == "metadata" && v != nil {
			templateMap[k] = processMetadataToMaps(v)
		} else if k == "spec" && v != nil {
			templateMap[k] = processSpecToMaps(v)
		}
	}
	return template
}

// mapToArray converts map[string]string to []Label
func mapToArray(value any) any {
	valueMap, ok := value.(map[string]any)
	if !ok {
		return value
	}

	labelArray := make([]map[string]any, 0, len(valueMap))
	for k, v := range valueMap {
		if strValue, ok := v.(string); ok {
			labelArray = append(labelArray, map[string]any{
				"key":   k,
				"value": strValue,
			})
		}
	}
	return labelArray
}

// arrayToMap converts []Label to map[string]string
func arrayToMap(value any) any {
	valueArray, ok := value.([]any)
	if !ok {
		return value
	}

	labelMap := make(map[string]string)
	for _, item := range valueArray {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}

		key, keyOk := itemMap["key"].(string)
		val, valOk := itemMap["value"].(string)
		if keyOk && valOk {
			labelMap[key] = val
		}
	}
	return labelMap
}

package gateway_test

type apps struct {
	Deployment       *deployment `json:"Deployment,omitempty"`
	CreateDeployment *deployment `json:"createDeployment,omitempty"`
	DeleteDeployment *bool       `json:"deleteDeployment,omitempty"`
}

type deployment struct {
	Metadata deploymentMetadata `json:"metadata"`
	Spec     deploymentSpec     `json:"spec"`
}

type deploymentMetadata struct {
	Name        string  `json:"name"`
	Namespace   string  `json:"namespace"`
	Labels      []label `json:"labels,omitempty"`
	Annotations []label `json:"annotations,omitempty"`
}

type label struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type deploymentSpec struct {
	Replicas int                `json:"replicas"`
	Selector deploymentSelector `json:"selector"`
	Template podTemplate        `json:"template"`
}

type deploymentSelector struct {
	MatchLabels []label `json:"matchLabels,omitempty"`
}

type podTemplate struct {
	Metadata podTemplateMetadata `json:"metadata"`
	Spec     podTemplateSpec     `json:"spec"`
}

type podTemplateMetadata struct {
	Labels []label `json:"labels,omitempty"`
}

type podTemplateSpec struct {
	NodeSelector []label               `json:"nodeSelector,omitempty"`
	Containers   []deploymentContainer `json:"containers"`
}

type deploymentContainer struct {
	Name  string           `json:"name"`
	Image string           `json:"image"`
	Ports []deploymentPort `json:"ports,omitempty"`
}

type deploymentPort struct {
	ContainerPort int `json:"containerPort"`
}

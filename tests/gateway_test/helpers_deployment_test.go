package gateway_test_test

type Spec struct {
	Replicas int `json:"replicas"`
}

type Deployment struct {
	Metadata Metadata `json:"metadata"`
	Spec     Spec     `json:"spec"`
}

type SubscriptionResponse struct {
	AppsDeployments []Deployment `json:"apps_deployments"`
}

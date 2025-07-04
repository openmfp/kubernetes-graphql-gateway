package clusteraccess

import (
	"context"
	"errors"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gatewayv1alpha1 "github.com/openmfp/kubernetes-graphql-gateway/common/apis/v1alpha1"
)

// extractCAData extracts CA certificate data from secret or configmap references
func extractCAData(ca *gatewayv1alpha1.CAConfig, k8sClient client.Client) ([]byte, error) {
	if ca == nil {
		return nil, nil
	}

	ctx := context.Background()

	if ca.SecretRef != nil {
		secret := &corev1.Secret{}
		namespace := ca.SecretRef.Namespace
		if namespace == "" {
			namespace = "default" // Use default namespace if not specified
		}

		err := k8sClient.Get(ctx, types.NamespacedName{
			Name:      ca.SecretRef.Name,
			Namespace: namespace,
		}, secret)
		if err != nil {
			return nil, errors.Join(errors.New("failed to get CA secret"), err)
		}

		caData, ok := secret.Data[ca.SecretRef.Key]
		if !ok {
			return nil, errors.New("CA key not found in secret")
		}

		return caData, nil
	}

	if ca.ConfigMapRef != nil {
		configMap := &corev1.ConfigMap{}
		namespace := ca.ConfigMapRef.Namespace
		if namespace == "" {
			namespace = "default"
		}

		err := k8sClient.Get(ctx, types.NamespacedName{
			Name:      ca.ConfigMapRef.Name,
			Namespace: namespace,
		}, configMap)
		if err != nil {
			return nil, errors.Join(errors.New("failed to get CA config map"), err)
		}

		caData, ok := configMap.Data[ca.ConfigMapRef.Key]
		if !ok {
			return nil, errors.New("CA key not found in config map")
		}

		return []byte(caData), nil
	}

	return nil, nil // No CA configuration
}

func configureAuthentication(config *rest.Config, auth *gatewayv1alpha1.AuthConfig, k8sClient client.Client) error {
	if auth == nil {
		return nil
	}

	ctx := context.Background()

	if auth.SecretRef != nil {
		secret := &corev1.Secret{}
		namespace := auth.SecretRef.Namespace
		if namespace == "" {
			namespace = "default"
		}

		err := k8sClient.Get(ctx, types.NamespacedName{
			Name:      auth.SecretRef.Name,
			Namespace: namespace,
		}, secret)
		if err != nil {
			return errors.Join(errors.New("failed to get auth secret"), err)
		}

		tokenData, ok := secret.Data[auth.SecretRef.Key]
		if !ok {
			return errors.New("auth key not found in secret")
		}

		config.BearerToken = string(tokenData)
		return nil
	}

	if auth.KubeconfigSecretRef != nil {
		secret := &corev1.Secret{}
		namespace := auth.KubeconfigSecretRef.Namespace
		if namespace == "" {
			namespace = "default"
		}

		err := k8sClient.Get(ctx, types.NamespacedName{
			Name:      auth.KubeconfigSecretRef.Name,
			Namespace: namespace,
		}, secret)
		if err != nil {
			return errors.Join(errors.New("failed to get kubeconfig secret"), err)
		}

		kubeconfigData, ok := secret.Data["kubeconfig"]
		if !ok {
			return errors.New("kubeconfig key not found in secret")
		}

		// Parse kubeconfig and extract auth info
		clientConfig, err := clientcmd.NewClientConfigFromBytes(kubeconfigData)
		if err != nil {
			return errors.Join(errors.New("failed to parse kubeconfig"), err)
		}

		rawConfig, err := clientConfig.RawConfig()
		if err != nil {
			return errors.Join(errors.New("failed to get raw kubeconfig"), err)
		}

		// Get the current context
		currentContext := rawConfig.CurrentContext
		if currentContext == "" {
			return errors.New("no current context in kubeconfig")
		}

		context, exists := rawConfig.Contexts[currentContext]
		if !exists {
			return errors.New("current context not found in kubeconfig")
		}

		// Get auth info for current context
		authInfo, exists := rawConfig.AuthInfos[context.AuthInfo]
		if !exists {
			return errors.New("auth info not found in kubeconfig")
		}

		// Extract authentication information
		if err := extractAuthFromKubeconfig(config, authInfo); err != nil {
			return errors.Join(errors.New("failed to extract auth from kubeconfig"), err)
		}

		return nil
	}

	if auth.ClientCertificateRef != nil {
		secret := &corev1.Secret{}
		namespace := auth.ClientCertificateRef.Namespace
		if namespace == "" {
			namespace = "default"
		}

		err := k8sClient.Get(ctx, types.NamespacedName{
			Name:      auth.ClientCertificateRef.Name,
			Namespace: namespace,
		}, secret)
		if err != nil {
			return errors.Join(errors.New("failed to get client certificate secret"), err)
		}

		certData, certOk := secret.Data["tls.crt"]
		keyData, keyOk := secret.Data["tls.key"]

		if !certOk || !keyOk {
			return errors.New("client certificate or key not found in secret")
		}

		config.TLSClientConfig.CertData = certData
		config.TLSClientConfig.KeyData = keyData
		return nil
	}

	if auth.ServiceAccount != "" {
		// TODO: Implement service account-based authentication
		return errors.New("service account authentication not yet implemented")
	}

	// No authentication configured - this might work for some clusters
	return nil
}

// extractAuthFromKubeconfig extracts authentication info from kubeconfig AuthInfo
func extractAuthFromKubeconfig(config *rest.Config, authInfo *api.AuthInfo) error {
	if authInfo.Token != "" {
		config.BearerToken = authInfo.Token
		return nil
	}

	if authInfo.TokenFile != "" {
		// TODO: Read token from file if needed
		return errors.New("token file authentication not yet implemented")
	}

	if len(authInfo.ClientCertificateData) > 0 && len(authInfo.ClientKeyData) > 0 {
		config.TLSClientConfig.CertData = authInfo.ClientCertificateData
		config.TLSClientConfig.KeyData = authInfo.ClientKeyData
		return nil
	}

	if authInfo.ClientCertificate != "" && authInfo.ClientKey != "" {
		config.TLSClientConfig.CertFile = authInfo.ClientCertificate
		config.TLSClientConfig.KeyFile = authInfo.ClientKey
		return nil
	}

	if authInfo.Username != "" && authInfo.Password != "" {
		config.Username = authInfo.Username
		config.Password = authInfo.Password
		return nil
	}

	// No recognizable authentication found
	return errors.New("no valid authentication method found in kubeconfig")
}

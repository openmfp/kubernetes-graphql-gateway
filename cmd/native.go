package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/go-openapi/spec"
	"github.com/graphql-go/handler"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openmfp/crd-gql-gateway/gateway"
	"github.com/openmfp/crd-gql-gateway/native"
	"github.com/openmfp/golang-commons/logger"
)

// getFilteredResourceMap returns a set of resource names allowed for filtering.
func getFilteredResourceMap() map[string]struct{} {
	return map[string]struct{}{
		"io.k8s.api.admissionregistration.v1.AuditAnnotation":                                        {},
		"io.k8s.api.admissionregistration.v1.ExpressionWarning":                                      {},
		"io.k8s.api.admissionregistration.v1.MatchCondition":                                         {},
		"io.k8s.api.admissionregistration.v1.MatchResources":                                         {},
		"io.k8s.api.admissionregistration.v1.MutatingWebhook":                                        {},
		"io.k8s.api.admissionregistration.v1.MutatingWebhookConfiguration":                           {},
		"io.k8s.api.admissionregistration.v1.MutatingWebhookConfigurationList":                       {},
		"io.k8s.api.admissionregistration.v1.NamedRuleWithOperations":                                {},
		"io.k8s.api.admissionregistration.v1.ParamKind":                                              {},
		"io.k8s.api.admissionregistration.v1.ParamRef":                                               {},
		"io.k8s.api.admissionregistration.v1.RuleWithOperations":                                     {},
		"io.k8s.api.admissionregistration.v1.ServiceReference":                                       {},
		"io.k8s.api.admissionregistration.v1.TypeChecking":                                           {},
		"io.k8s.api.admissionregistration.v1.ValidatingAdmissionPolicy":                              {},
		"io.k8s.api.admissionregistration.v1.ValidatingAdmissionPolicyBinding":                       {},
		"io.k8s.api.admissionregistration.v1.ValidatingAdmissionPolicyBindingList":                   {},
		"io.k8s.api.admissionregistration.v1.ValidatingAdmissionPolicyBindingSpec":                   {},
		"io.k8s.api.admissionregistration.v1.ValidatingAdmissionPolicyList":                          {},
		"io.k8s.api.admissionregistration.v1.ValidatingAdmissionPolicySpec":                          {},
		"io.k8s.api.admissionregistration.v1.ValidatingAdmissionPolicyStatus":                        {},
		"io.k8s.api.admissionregistration.v1.ValidatingWebhook":                                      {},
		"io.k8s.api.admissionregistration.v1.ValidatingWebhookConfiguration":                         {},
		"io.k8s.api.admissionregistration.v1.ValidatingWebhookConfigurationList":                     {},
		"io.k8s.api.admissionregistration.v1.Validation":                                             {},
		"io.k8s.api.admissionregistration.v1.Variable":                                               {},
		"io.k8s.api.admissionregistration.v1.WebhookClientConfig":                                    {},
		"io.k8s.api.apps.v1.ControllerRevision":                                                      {},
		"io.k8s.api.apps.v1.ControllerRevisionList":                                                  {},
		"io.k8s.api.apps.v1.DaemonSet":                                                               {},
		"io.k8s.api.apps.v1.DaemonSetCondition":                                                      {},
		"io.k8s.api.apps.v1.DaemonSetList":                                                           {},
		"io.k8s.api.apps.v1.DaemonSetSpec":                                                           {},
		"io.k8s.api.apps.v1.DaemonSetStatus":                                                         {},
		"io.k8s.api.apps.v1.DaemonSetUpdateStrategy":                                                 {},
		"io.k8s.api.apps.v1.Deployment":                                                              {},
		"io.k8s.api.apps.v1.DeploymentCondition":                                                     {},
		"io.k8s.api.apps.v1.DeploymentList":                                                          {},
		"io.k8s.api.apps.v1.DeploymentSpec":                                                          {},
		"io.k8s.api.apps.v1.DeploymentStatus":                                                        {},
		"io.k8s.api.apps.v1.DeploymentStrategy":                                                      {},
		"io.k8s.api.apps.v1.ReplicaSet":                                                              {},
		"io.k8s.api.apps.v1.ReplicaSetCondition":                                                     {},
		"io.k8s.api.apps.v1.ReplicaSetList":                                                          {},
		"io.k8s.api.apps.v1.ReplicaSetSpec":                                                          {},
		"io.k8s.api.apps.v1.ReplicaSetStatus":                                                        {},
		"io.k8s.api.apps.v1.RollingUpdateDaemonSet":                                                  {},
		"io.k8s.api.apps.v1.RollingUpdateDeployment":                                                 {},
		"io.k8s.api.apps.v1.RollingUpdateStatefulSetStrategy":                                        {},
		"io.k8s.api.apps.v1.StatefulSet":                                                             {},
		"io.k8s.api.apps.v1.StatefulSetCondition":                                                    {},
		"io.k8s.api.apps.v1.StatefulSetList":                                                         {},
		"io.k8s.api.apps.v1.StatefulSetOrdinals":                                                     {},
		"io.k8s.api.apps.v1.StatefulSetPersistentVolumeClaimRetentionPolicy":                         {},
		"io.k8s.api.apps.v1.StatefulSetSpec":                                                         {},
		"io.k8s.api.apps.v1.StatefulSetStatus":                                                       {},
		"io.k8s.api.apps.v1.StatefulSetUpdateStrategy":                                               {},
		"io.k8s.api.authentication.v1.BoundObjectReference":                                          {},
		"io.k8s.api.authentication.v1.SelfSubjectReview":                                             {},
		"io.k8s.api.authentication.v1.SelfSubjectReviewStatus":                                       {},
		"io.k8s.api.authentication.v1.TokenRequest":                                                  {},
		"io.k8s.api.authentication.v1.TokenRequestSpec":                                              {},
		"io.k8s.api.authentication.v1.TokenRequestStatus":                                            {},
		"io.k8s.api.authentication.v1.TokenReview":                                                   {},
		"io.k8s.api.authentication.v1.TokenReviewSpec":                                               {},
		"io.k8s.api.authentication.v1.TokenReviewStatus":                                             {},
		"io.k8s.api.authentication.v1.UserInfo":                                                      {},
		"io.k8s.api.authorization.v1.LocalSubjectAccessReview":                                       {},
		"io.k8s.api.authorization.v1.NonResourceAttributes":                                          {},
		"io.k8s.api.authorization.v1.NonResourceRule":                                                {},
		"io.k8s.api.authorization.v1.ResourceAttributes":                                             {},
		"io.k8s.api.authorization.v1.ResourceRule":                                                   {},
		"io.k8s.api.authorization.v1.SelfSubjectAccessReview":                                        {},
		"io.k8s.api.authorization.v1.SelfSubjectAccessReviewSpec":                                    {},
		"io.k8s.api.authorization.v1.SelfSubjectRulesReview":                                         {},
		"io.k8s.api.authorization.v1.SelfSubjectRulesReviewSpec":                                     {},
		"io.k8s.api.authorization.v1.SubjectAccessReview":                                            {},
		"io.k8s.api.authorization.v1.SubjectAccessReviewSpec":                                        {},
		"io.k8s.api.authorization.v1.SubjectAccessReviewStatus":                                      {},
		"io.k8s.api.authorization.v1.SubjectRulesReviewStatus":                                       {},
		"io.k8s.api.autoscaling.v1.CrossVersionObjectReference":                                      {},
		"io.k8s.api.autoscaling.v1.HorizontalPodAutoscaler":                                          {},
		"io.k8s.api.autoscaling.v1.HorizontalPodAutoscalerList":                                      {},
		"io.k8s.api.autoscaling.v1.HorizontalPodAutoscalerSpec":                                      {},
		"io.k8s.api.autoscaling.v1.HorizontalPodAutoscalerStatus":                                    {},
		"io.k8s.api.autoscaling.v1.Scale":                                                            {},
		"io.k8s.api.autoscaling.v1.ScaleSpec":                                                        {},
		"io.k8s.api.autoscaling.v1.ScaleStatus":                                                      {},
		"io.k8s.api.autoscaling.v2.ContainerResourceMetricSource":                                    {},
		"io.k8s.api.autoscaling.v2.ContainerResourceMetricStatus":                                    {},
		"io.k8s.api.autoscaling.v2.CrossVersionObjectReference":                                      {},
		"io.k8s.api.autoscaling.v2.ExternalMetricSource":                                             {},
		"io.k8s.api.autoscaling.v2.ExternalMetricStatus":                                             {},
		"io.k8s.api.autoscaling.v2.HPAScalingPolicy":                                                 {},
		"io.k8s.api.autoscaling.v2.HPAScalingRules":                                                  {},
		"io.k8s.api.autoscaling.v2.HorizontalPodAutoscaler":                                          {},
		"io.k8s.api.autoscaling.v2.HorizontalPodAutoscalerBehavior":                                  {},
		"io.k8s.api.autoscaling.v2.HorizontalPodAutoscalerCondition":                                 {},
		"io.k8s.api.autoscaling.v2.HorizontalPodAutoscalerList":                                      {},
		"io.k8s.api.autoscaling.v2.HorizontalPodAutoscalerSpec":                                      {},
		"io.k8s.api.autoscaling.v2.HorizontalPodAutoscalerStatus":                                    {},
		"io.k8s.api.autoscaling.v2.MetricIdentifier":                                                 {},
		"io.k8s.api.autoscaling.v2.MetricSpec":                                                       {},
		"io.k8s.api.autoscaling.v2.MetricStatus":                                                     {},
		"io.k8s.api.autoscaling.v2.MetricTarget":                                                     {},
		"io.k8s.api.autoscaling.v2.MetricValueStatus":                                                {},
		"io.k8s.api.autoscaling.v2.ObjectMetricSource":                                               {},
		"io.k8s.api.autoscaling.v2.ObjectMetricStatus":                                               {},
		"io.k8s.api.autoscaling.v2.PodsMetricSource":                                                 {},
		"io.k8s.api.autoscaling.v2.PodsMetricStatus":                                                 {},
		"io.k8s.api.autoscaling.v2.ResourceMetricSource":                                             {},
		"io.k8s.api.autoscaling.v2.ResourceMetricStatus":                                             {},
		"io.k8s.api.batch.v1.CronJob":                                                                {},
		"io.k8s.api.batch.v1.CronJobList":                                                            {},
		"io.k8s.api.batch.v1.CronJobSpec":                                                            {},
		"io.k8s.api.batch.v1.CronJobStatus":                                                          {},
		"io.k8s.api.batch.v1.Job":                                                                    {},
		"io.k8s.api.batch.v1.JobCondition":                                                           {},
		"io.k8s.api.batch.v1.JobList":                                                                {},
		"io.k8s.api.batch.v1.JobSpec":                                                                {},
		"io.k8s.api.batch.v1.JobStatus":                                                              {},
		"io.k8s.api.batch.v1.JobTemplateSpec":                                                        {},
		"io.k8s.api.batch.v1.PodFailurePolicy":                                                       {},
		"io.k8s.api.batch.v1.PodFailurePolicyOnExitCodesRequirement":                                 {},
		"io.k8s.api.batch.v1.PodFailurePolicyOnPodConditionsPattern":                                 {},
		"io.k8s.api.batch.v1.PodFailurePolicyRule":                                                   {},
		"io.k8s.api.batch.v1.SuccessPolicy":                                                          {},
		"io.k8s.api.batch.v1.SuccessPolicyRule":                                                      {},
		"io.k8s.api.batch.v1.UncountedTerminatedPods":                                                {},
		"io.k8s.api.certificates.v1.CertificateSigningRequest":                                       {},
		"io.k8s.api.certificates.v1.CertificateSigningRequestCondition":                              {},
		"io.k8s.api.certificates.v1.CertificateSigningRequestList":                                   {},
		"io.k8s.api.certificates.v1.CertificateSigningRequestSpec":                                   {},
		"io.k8s.api.certificates.v1.CertificateSigningRequestStatus":                                 {},
		"io.k8s.api.coordination.v1.Lease":                                                           {},
		"io.k8s.api.coordination.v1.LeaseList":                                                       {},
		"io.k8s.api.coordination.v1.LeaseSpec":                                                       {},
		"io.k8s.api.core.v1.AWSElasticBlockStoreVolumeSource":                                        {},
		"io.k8s.api.core.v1.Affinity":                                                                {},
		"io.k8s.api.core.v1.AppArmorProfile":                                                         {},
		"io.k8s.api.core.v1.AttachedVolume":                                                          {},
		"io.k8s.api.core.v1.AzureDiskVolumeSource":                                                   {},
		"io.k8s.api.core.v1.AzureFilePersistentVolumeSource":                                         {},
		"io.k8s.api.core.v1.AzureFileVolumeSource":                                                   {},
		"io.k8s.api.core.v1.Binding":                                                                 {},
		"io.k8s.api.core.v1.CSIPersistentVolumeSource":                                               {},
		"io.k8s.api.core.v1.CSIVolumeSource":                                                         {},
		"io.k8s.api.core.v1.Capabilities":                                                            {},
		"io.k8s.api.core.v1.CephFSPersistentVolumeSource":                                            {},
		"io.k8s.api.core.v1.CephFSVolumeSource":                                                      {},
		"io.k8s.api.core.v1.CinderPersistentVolumeSource":                                            {},
		"io.k8s.api.core.v1.CinderVolumeSource":                                                      {},
		"io.k8s.api.core.v1.ClaimSource":                                                             {},
		"io.k8s.api.core.v1.ClientIPConfig":                                                          {},
		"io.k8s.api.core.v1.ClusterTrustBundleProjection":                                            {},
		"io.k8s.api.core.v1.ComponentCondition":                                                      {},
		"io.k8s.api.core.v1.ComponentStatus":                                                         {},
		"io.k8s.api.core.v1.ComponentStatusList":                                                     {},
		"io.k8s.api.core.v1.ConfigMap":                                                               {},
		"io.k8s.api.core.v1.ConfigMapEnvSource":                                                      {},
		"io.k8s.api.core.v1.ConfigMapKeySelector":                                                    {},
		"io.k8s.api.core.v1.ConfigMapList":                                                           {},
		"io.k8s.api.core.v1.ConfigMapNodeConfigSource":                                               {},
		"io.k8s.api.core.v1.ConfigMapProjection":                                                     {},
		"io.k8s.api.core.v1.ConfigMapVolumeSource":                                                   {},
		"io.k8s.api.core.v1.Container":                                                               {},
		"io.k8s.api.core.v1.ContainerImage":                                                          {},
		"io.k8s.api.core.v1.ContainerPort":                                                           {},
		"io.k8s.api.core.v1.ContainerResizePolicy":                                                   {},
		"io.k8s.api.core.v1.ContainerState":                                                          {},
		"io.k8s.api.core.v1.ContainerStateRunning":                                                   {},
		"io.k8s.api.core.v1.ContainerStateTerminated":                                                {},
		"io.k8s.api.core.v1.ContainerStateWaiting":                                                   {},
		"io.k8s.api.core.v1.ContainerStatus":                                                         {},
		"io.k8s.api.core.v1.DaemonEndpoint":                                                          {},
		"io.k8s.api.core.v1.DownwardAPIProjection":                                                   {},
		"io.k8s.api.core.v1.DownwardAPIVolumeFile":                                                   {},
		"io.k8s.api.core.v1.DownwardAPIVolumeSource":                                                 {},
		"io.k8s.api.core.v1.EmptyDirVolumeSource":                                                    {},
		"io.k8s.api.core.v1.EndpointAddress":                                                         {},
		"io.k8s.api.core.v1.EndpointPort":                                                            {},
		"io.k8s.api.core.v1.EndpointSubset":                                                          {},
		"io.k8s.api.core.v1.Endpoints":                                                               {},
		"io.k8s.api.core.v1.EndpointsList":                                                           {},
		"io.k8s.api.core.v1.EnvFromSource":                                                           {},
		"io.k8s.api.core.v1.EnvVar":                                                                  {},
		"io.k8s.api.core.v1.EnvVarSource":                                                            {},
		"io.k8s.api.core.v1.EphemeralContainer":                                                      {},
		"io.k8s.api.core.v1.EphemeralVolumeSource":                                                   {},
		"io.k8s.api.core.v1.Event":                                                                   {},
		"io.k8s.api.core.v1.EventList":                                                               {},
		"io.k8s.api.core.v1.EventSeries":                                                             {},
		"io.k8s.api.core.v1.EventSource":                                                             {},
		"io.k8s.api.core.v1.ExecAction":                                                              {},
		"io.k8s.api.core.v1.FCVolumeSource":                                                          {},
		"io.k8s.api.core.v1.FlexPersistentVolumeSource":                                              {},
		"io.k8s.api.core.v1.FlexVolumeSource":                                                        {},
		"io.k8s.api.core.v1.FlockerVolumeSource":                                                     {},
		"io.k8s.api.core.v1.GCEPersistentDiskVolumeSource":                                           {},
		"io.k8s.api.core.v1.GRPCAction":                                                              {},
		"io.k8s.api.core.v1.GitRepoVolumeSource":                                                     {},
		"io.k8s.api.core.v1.GlusterfsPersistentVolumeSource":                                         {},
		"io.k8s.api.core.v1.GlusterfsVolumeSource":                                                   {},
		"io.k8s.api.core.v1.HTTPGetAction":                                                           {},
		"io.k8s.api.core.v1.HTTPHeader":                                                              {},
		"io.k8s.api.core.v1.HostAlias":                                                               {},
		"io.k8s.api.core.v1.HostIP":                                                                  {},
		"io.k8s.api.core.v1.HostPathVolumeSource":                                                    {},
		"io.k8s.api.core.v1.ISCSIPersistentVolumeSource":                                             {},
		"io.k8s.api.core.v1.ISCSIVolumeSource":                                                       {},
		"io.k8s.api.core.v1.KeyToPath":                                                               {},
		"io.k8s.api.core.v1.Lifecycle":                                                               {},
		"io.k8s.api.core.v1.LifecycleHandler":                                                        {},
		"io.k8s.api.core.v1.LimitRange":                                                              {},
		"io.k8s.api.core.v1.LimitRangeItem":                                                          {},
		"io.k8s.api.core.v1.LimitRangeList":                                                          {},
		"io.k8s.api.core.v1.LimitRangeSpec":                                                          {},
		"io.k8s.api.core.v1.LoadBalancerIngress":                                                     {},
		"io.k8s.api.core.v1.LoadBalancerStatus":                                                      {},
		"io.k8s.api.core.v1.LocalObjectReference":                                                    {},
		"io.k8s.api.core.v1.LocalVolumeSource":                                                       {},
		"io.k8s.api.core.v1.ModifyVolumeStatus":                                                      {},
		"io.k8s.api.core.v1.NFSVolumeSource":                                                         {},
		"io.k8s.api.core.v1.Namespace":                                                               {},
		"io.k8s.api.core.v1.NamespaceCondition":                                                      {},
		"io.k8s.api.core.v1.NamespaceList":                                                           {},
		"io.k8s.api.core.v1.NamespaceSpec":                                                           {},
		"io.k8s.api.core.v1.NamespaceStatus":                                                         {},
		"io.k8s.api.core.v1.Node":                                                                    {},
		"io.k8s.api.core.v1.NodeAddress":                                                             {},
		"io.k8s.api.core.v1.NodeAffinity":                                                            {},
		"io.k8s.api.core.v1.NodeCondition":                                                           {},
		"io.k8s.api.core.v1.NodeConfigSource":                                                        {},
		"io.k8s.api.core.v1.NodeConfigStatus":                                                        {},
		"io.k8s.api.core.v1.NodeDaemonEndpoints":                                                     {},
		"io.k8s.api.core.v1.NodeList":                                                                {},
		"io.k8s.api.core.v1.NodeRuntimeHandler":                                                      {},
		"io.k8s.api.core.v1.NodeRuntimeHandlerFeatures":                                              {},
		"io.k8s.api.core.v1.NodeSelector":                                                            {},
		"io.k8s.api.core.v1.NodeSelectorRequirement":                                                 {},
		"io.k8s.api.core.v1.NodeSelectorTerm":                                                        {},
		"io.k8s.api.core.v1.NodeSpec":                                                                {},
		"io.k8s.api.core.v1.NodeStatus":                                                              {},
		"io.k8s.api.core.v1.NodeSystemInfo":                                                          {},
		"io.k8s.api.core.v1.ObjectFieldSelector":                                                     {},
		"io.k8s.api.core.v1.ObjectReference":                                                         {},
		"io.k8s.api.core.v1.PersistentVolume":                                                        {},
		"io.k8s.api.core.v1.PersistentVolumeClaim":                                                   {},
		"io.k8s.api.core.v1.PersistentVolumeClaimCondition":                                          {},
		"io.k8s.api.core.v1.PersistentVolumeClaimList":                                               {},
		"io.k8s.api.core.v1.PersistentVolumeClaimSpec":                                               {},
		"io.k8s.api.core.v1.PersistentVolumeClaimStatus":                                             {},
		"io.k8s.api.core.v1.PersistentVolumeClaimTemplate":                                           {},
		"io.k8s.api.core.v1.PersistentVolumeClaimVolumeSource":                                       {},
		"io.k8s.api.core.v1.PersistentVolumeList":                                                    {},
		"io.k8s.api.core.v1.PersistentVolumeSpec":                                                    {},
		"io.k8s.api.core.v1.PersistentVolumeStatus":                                                  {},
		"io.k8s.api.core.v1.PhotonPersistentDiskVolumeSource":                                        {},
		"io.k8s.api.core.v1.Pod":                                                                     {},
		"io.k8s.api.core.v1.PodAffinity":                                                             {},
		"io.k8s.api.core.v1.PodAffinityTerm":                                                         {},
		"io.k8s.api.core.v1.PodAntiAffinity":                                                         {},
		"io.k8s.api.core.v1.PodCondition":                                                            {},
		"io.k8s.api.core.v1.PodDNSConfig":                                                            {},
		"io.k8s.api.core.v1.PodDNSConfigOption":                                                      {},
		"io.k8s.api.core.v1.PodIP":                                                                   {},
		"io.k8s.api.core.v1.PodList":                                                                 {},
		"io.k8s.api.core.v1.PodOS":                                                                   {},
		"io.k8s.api.core.v1.PodReadinessGate":                                                        {},
		"io.k8s.api.core.v1.PodResourceClaim":                                                        {},
		"io.k8s.api.core.v1.PodResourceClaimStatus":                                                  {},
		"io.k8s.api.core.v1.PodSchedulingGate":                                                       {},
		"io.k8s.api.core.v1.PodSecurityContext":                                                      {},
		"io.k8s.api.core.v1.PodSpec":                                                                 {},
		"io.k8s.api.core.v1.PodStatus":                                                               {},
		"io.k8s.api.core.v1.PodTemplate":                                                             {},
		"io.k8s.api.core.v1.PodTemplateList":                                                         {},
		"io.k8s.api.core.v1.PodTemplateSpec":                                                         {},
		"io.k8s.api.core.v1.PortStatus":                                                              {},
		"io.k8s.api.core.v1.PortworxVolumeSource":                                                    {},
		"io.k8s.api.core.v1.PreferredSchedulingTerm":                                                 {},
		"io.k8s.api.core.v1.Probe":                                                                   {},
		"io.k8s.api.core.v1.ProjectedVolumeSource":                                                   {},
		"io.k8s.api.core.v1.QuobyteVolumeSource":                                                     {},
		"io.k8s.api.core.v1.RBDPersistentVolumeSource":                                               {},
		"io.k8s.api.core.v1.RBDVolumeSource":                                                         {},
		"io.k8s.api.core.v1.ReplicationController":                                                   {},
		"io.k8s.api.core.v1.ReplicationControllerCondition":                                          {},
		"io.k8s.api.core.v1.ReplicationControllerList":                                               {},
		"io.k8s.api.core.v1.ReplicationControllerSpec":                                               {},
		"io.k8s.api.core.v1.ReplicationControllerStatus":                                             {},
		"io.k8s.api.core.v1.ResourceClaim":                                                           {},
		"io.k8s.api.core.v1.ResourceFieldSelector":                                                   {},
		"io.k8s.api.core.v1.ResourceQuota":                                                           {},
		"io.k8s.api.core.v1.ResourceQuotaList":                                                       {},
		"io.k8s.api.core.v1.ResourceQuotaSpec":                                                       {},
		"io.k8s.api.core.v1.ResourceQuotaStatus":                                                     {},
		"io.k8s.api.core.v1.ResourceRequirements":                                                    {},
		"io.k8s.api.core.v1.SELinuxOptions":                                                          {},
		"io.k8s.api.core.v1.ScaleIOPersistentVolumeSource":                                           {},
		"io.k8s.api.core.v1.ScaleIOVolumeSource":                                                     {},
		"io.k8s.api.core.v1.ScopeSelector":                                                           {},
		"io.k8s.api.core.v1.ScopedResourceSelectorRequirement":                                       {},
		"io.k8s.api.core.v1.SeccompProfile":                                                          {},
		"io.k8s.api.core.v1.Secret":                                                                  {},
		"io.k8s.api.core.v1.SecretEnvSource":                                                         {},
		"io.k8s.api.core.v1.SecretKeySelector":                                                       {},
		"io.k8s.api.core.v1.SecretList":                                                              {},
		"io.k8s.api.core.v1.SecretProjection":                                                        {},
		"io.k8s.api.core.v1.SecretReference":                                                         {},
		"io.k8s.api.core.v1.SecretVolumeSource":                                                      {},
		"io.k8s.api.core.v1.SecurityContext":                                                         {},
		"io.k8s.api.core.v1.Service":                                                                 {},
		"io.k8s.api.core.v1.ServiceAccount":                                                          {},
		"io.k8s.api.core.v1.ServiceAccountList":                                                      {},
		"io.k8s.api.core.v1.ServiceAccountTokenProjection":                                           {},
		"io.k8s.api.core.v1.ServiceList":                                                             {},
		"io.k8s.api.core.v1.ServicePort":                                                             {},
		"io.k8s.api.core.v1.ServiceSpec":                                                             {},
		"io.k8s.api.core.v1.ServiceStatus":                                                           {},
		"io.k8s.api.core.v1.SessionAffinityConfig":                                                   {},
		"io.k8s.api.core.v1.SleepAction":                                                             {},
		"io.k8s.api.core.v1.StorageOSPersistentVolumeSource":                                         {},
		"io.k8s.api.core.v1.StorageOSVolumeSource":                                                   {},
		"io.k8s.api.core.v1.Sysctl":                                                                  {},
		"io.k8s.api.core.v1.TCPSocketAction":                                                         {},
		"io.k8s.api.core.v1.Taint":                                                                   {},
		"io.k8s.api.core.v1.Toleration":                                                              {},
		"io.k8s.api.core.v1.TopologySelectorLabelRequirement":                                        {},
		"io.k8s.api.core.v1.TopologySelectorTerm":                                                    {},
		"io.k8s.api.core.v1.TopologySpreadConstraint":                                                {},
		"io.k8s.api.core.v1.TypedLocalObjectReference":                                               {},
		"io.k8s.api.core.v1.TypedObjectReference":                                                    {},
		"io.k8s.api.core.v1.Volume":                                                                  {},
		"io.k8s.api.core.v1.VolumeDevice":                                                            {},
		"io.k8s.api.core.v1.VolumeMount":                                                             {},
		"io.k8s.api.core.v1.VolumeMountStatus":                                                       {},
		"io.k8s.api.core.v1.VolumeNodeAffinity":                                                      {},
		"io.k8s.api.core.v1.VolumeProjection":                                                        {},
		"io.k8s.api.core.v1.VolumeResourceRequirements":                                              {},
		"io.k8s.api.core.v1.VsphereVirtualDiskVolumeSource":                                          {},
		"io.k8s.api.core.v1.WeightedPodAffinityTerm":                                                 {},
		"io.k8s.api.core.v1.WindowsSecurityContextOptions":                                           {},
		"io.k8s.api.discovery.v1.Endpoint":                                                           {},
		"io.k8s.api.discovery.v1.EndpointConditions":                                                 {},
		"io.k8s.api.discovery.v1.EndpointHints":                                                      {},
		"io.k8s.api.discovery.v1.EndpointPort":                                                       {},
		"io.k8s.api.discovery.v1.EndpointSlice":                                                      {},
		"io.k8s.api.discovery.v1.EndpointSliceList":                                                  {},
		"io.k8s.api.discovery.v1.ForZone":                                                            {},
		"io.k8s.api.events.v1.Event":                                                                 {},
		"io.k8s.api.events.v1.EventList":                                                             {},
		"io.k8s.api.events.v1.EventSeries":                                                           {},
		"io.k8s.api.flowcontrol.v1.ExemptPriorityLevelConfiguration":                                 {},
		"io.k8s.api.flowcontrol.v1.FlowDistinguisherMethod":                                          {},
		"io.k8s.api.flowcontrol.v1.FlowSchema":                                                       {},
		"io.k8s.api.flowcontrol.v1.FlowSchemaCondition":                                              {},
		"io.k8s.api.flowcontrol.v1.FlowSchemaList":                                                   {},
		"io.k8s.api.flowcontrol.v1.FlowSchemaSpec":                                                   {},
		"io.k8s.api.flowcontrol.v1.FlowSchemaStatus":                                                 {},
		"io.k8s.api.flowcontrol.v1.GroupSubject":                                                     {},
		"io.k8s.api.flowcontrol.v1.LimitResponse":                                                    {},
		"io.k8s.api.flowcontrol.v1.LimitedPriorityLevelConfiguration":                                {},
		"io.k8s.api.flowcontrol.v1.NonResourcePolicyRule":                                            {},
		"io.k8s.api.flowcontrol.v1.PolicyRulesWithSubjects":                                          {},
		"io.k8s.api.flowcontrol.v1.PriorityLevelConfiguration":                                       {},
		"io.k8s.api.flowcontrol.v1.PriorityLevelConfigurationCondition":                              {},
		"io.k8s.api.flowcontrol.v1.PriorityLevelConfigurationList":                                   {},
		"io.k8s.api.flowcontrol.v1.PriorityLevelConfigurationReference":                              {},
		"io.k8s.api.flowcontrol.v1.PriorityLevelConfigurationSpec":                                   {},
		"io.k8s.api.flowcontrol.v1.PriorityLevelConfigurationStatus":                                 {},
		"io.k8s.api.flowcontrol.v1.QueuingConfiguration":                                             {},
		"io.k8s.api.flowcontrol.v1.ResourcePolicyRule":                                               {},
		"io.k8s.api.flowcontrol.v1.ServiceAccountSubject":                                            {},
		"io.k8s.api.flowcontrol.v1.Subject":                                                          {},
		"io.k8s.api.flowcontrol.v1.UserSubject":                                                      {},
		"io.k8s.api.flowcontrol.v1beta3.ExemptPriorityLevelConfiguration":                            {},
		"io.k8s.api.flowcontrol.v1beta3.FlowDistinguisherMethod":                                     {},
		"io.k8s.api.flowcontrol.v1beta3.FlowSchema":                                                  {},
		"io.k8s.api.flowcontrol.v1beta3.FlowSchemaCondition":                                         {},
		"io.k8s.api.flowcontrol.v1beta3.FlowSchemaList":                                              {},
		"io.k8s.api.flowcontrol.v1beta3.FlowSchemaSpec":                                              {},
		"io.k8s.api.flowcontrol.v1beta3.FlowSchemaStatus":                                            {},
		"io.k8s.api.flowcontrol.v1beta3.GroupSubject":                                                {},
		"io.k8s.api.flowcontrol.v1beta3.LimitResponse":                                               {},
		"io.k8s.api.flowcontrol.v1beta3.LimitedPriorityLevelConfiguration":                           {},
		"io.k8s.api.flowcontrol.v1beta3.NonResourcePolicyRule":                                       {},
		"io.k8s.api.flowcontrol.v1beta3.PolicyRulesWithSubjects":                                     {},
		"io.k8s.api.flowcontrol.v1beta3.PriorityLevelConfiguration":                                  {},
		"io.k8s.api.flowcontrol.v1beta3.PriorityLevelConfigurationCondition":                         {},
		"io.k8s.api.flowcontrol.v1beta3.PriorityLevelConfigurationList":                              {},
		"io.k8s.api.flowcontrol.v1beta3.PriorityLevelConfigurationReference":                         {},
		"io.k8s.api.flowcontrol.v1beta3.PriorityLevelConfigurationSpec":                              {},
		"io.k8s.api.flowcontrol.v1beta3.PriorityLevelConfigurationStatus":                            {},
		"io.k8s.api.flowcontrol.v1beta3.QueuingConfiguration":                                        {},
		"io.k8s.api.flowcontrol.v1beta3.ResourcePolicyRule":                                          {},
		"io.k8s.api.flowcontrol.v1beta3.ServiceAccountSubject":                                       {},
		"io.k8s.api.flowcontrol.v1beta3.Subject":                                                     {},
		"io.k8s.api.flowcontrol.v1beta3.UserSubject":                                                 {},
		"io.k8s.api.networking.v1.HTTPIngressPath":                                                   {},
		"io.k8s.api.networking.v1.HTTPIngressRuleValue":                                              {},
		"io.k8s.api.networking.v1.IPBlock":                                                           {},
		"io.k8s.api.networking.v1.Ingress":                                                           {},
		"io.k8s.api.networking.v1.IngressBackend":                                                    {},
		"io.k8s.api.networking.v1.IngressClass":                                                      {},
		"io.k8s.api.networking.v1.IngressClassList":                                                  {},
		"io.k8s.api.networking.v1.IngressClassParametersReference":                                   {},
		"io.k8s.api.networking.v1.IngressClassSpec":                                                  {},
		"io.k8s.api.networking.v1.IngressList":                                                       {},
		"io.k8s.api.networking.v1.IngressLoadBalancerIngress":                                        {},
		"io.k8s.api.networking.v1.IngressLoadBalancerStatus":                                         {},
		"io.k8s.api.networking.v1.IngressPortStatus":                                                 {},
		"io.k8s.api.networking.v1.IngressRule":                                                       {},
		"io.k8s.api.networking.v1.IngressServiceBackend":                                             {},
		"io.k8s.api.networking.v1.IngressSpec":                                                       {},
		"io.k8s.api.networking.v1.IngressStatus":                                                     {},
		"io.k8s.api.networking.v1.IngressTLS":                                                        {},
		"io.k8s.api.networking.v1.NetworkPolicy":                                                     {},
		"io.k8s.api.networking.v1.NetworkPolicyEgressRule":                                           {},
		"io.k8s.api.networking.v1.NetworkPolicyIngressRule":                                          {},
		"io.k8s.api.networking.v1.NetworkPolicyList":                                                 {},
		"io.k8s.api.networking.v1.NetworkPolicyPeer":                                                 {},
		"io.k8s.api.networking.v1.NetworkPolicyPort":                                                 {},
		"io.k8s.api.networking.v1.NetworkPolicySpec":                                                 {},
		"io.k8s.api.networking.v1.ServiceBackendPort":                                                {},
		"io.k8s.api.node.v1.Overhead":                                                                {},
		"io.k8s.api.node.v1.RuntimeClass":                                                            {},
		"io.k8s.api.node.v1.RuntimeClassList":                                                        {},
		"io.k8s.api.node.v1.Scheduling":                                                              {},
		"io.k8s.api.policy.v1.Eviction":                                                              {},
		"io.k8s.api.policy.v1.PodDisruptionBudget":                                                   {},
		"io.k8s.api.policy.v1.PodDisruptionBudgetList":                                               {},
		"io.k8s.api.policy.v1.PodDisruptionBudgetSpec":                                               {},
		"io.k8s.api.policy.v1.PodDisruptionBudgetStatus":                                             {},
		"io.k8s.api.rbac.v1.AggregationRule":                                                         {},
		"io.k8s.api.rbac.v1.ClusterRole":                                                             {},
		"io.k8s.api.rbac.v1.ClusterRoleBinding":                                                      {},
		"io.k8s.api.rbac.v1.ClusterRoleBindingList":                                                  {},
		"io.k8s.api.rbac.v1.ClusterRoleList":                                                         {},
		"io.k8s.api.rbac.v1.PolicyRule":                                                              {},
		"io.k8s.api.rbac.v1.Role":                                                                    {},
		"io.k8s.api.rbac.v1.RoleBinding":                                                             {},
		"io.k8s.api.rbac.v1.RoleBindingList":                                                         {},
		"io.k8s.api.rbac.v1.RoleList":                                                                {},
		"io.k8s.api.rbac.v1.RoleRef":                                                                 {},
		"io.k8s.api.rbac.v1.Subject":                                                                 {},
		"io.k8s.api.scheduling.v1.PriorityClass":                                                     {},
		"io.k8s.api.scheduling.v1.PriorityClassList":                                                 {},
		"io.k8s.api.storage.v1.CSIDriver":                                                            {},
		"io.k8s.api.storage.v1.CSIDriverList":                                                        {},
		"io.k8s.api.storage.v1.CSIDriverSpec":                                                        {},
		"io.k8s.api.storage.v1.CSINode":                                                              {},
		"io.k8s.api.storage.v1.CSINodeDriver":                                                        {},
		"io.k8s.api.storage.v1.CSINodeList":                                                          {},
		"io.k8s.api.storage.v1.CSINodeSpec":                                                          {},
		"io.k8s.api.storage.v1.CSIStorageCapacity":                                                   {},
		"io.k8s.api.storage.v1.CSIStorageCapacityList":                                               {},
		"io.k8s.api.storage.v1.StorageClass":                                                         {},
		"io.k8s.api.storage.v1.StorageClassList":                                                     {},
		"io.k8s.api.storage.v1.TokenRequest":                                                         {},
		"io.k8s.api.storage.v1.VolumeAttachment":                                                     {},
		"io.k8s.api.storage.v1.VolumeAttachmentList":                                                 {},
		"io.k8s.api.storage.v1.VolumeAttachmentSource":                                               {},
		"io.k8s.api.storage.v1.VolumeAttachmentSpec":                                                 {},
		"io.k8s.api.storage.v1.VolumeAttachmentStatus":                                               {},
		"io.k8s.api.storage.v1.VolumeError":                                                          {},
		"io.k8s.api.storage.v1.VolumeNodeResources":                                                  {},
		"io.k8s.apiextensions-apiserver.pkg.apis.apiextensions.v1.CustomResourceColumnDefinition":    {},
		"io.k8s.apiextensions-apiserver.pkg.apis.apiextensions.v1.CustomResourceConversion":          {},
		"io.k8s.apiextensions-apiserver.pkg.apis.apiextensions.v1.CustomResourceDefinition":          {},
		"io.k8s.apiextensions-apiserver.pkg.apis.apiextensions.v1.CustomResourceDefinitionCondition": {},
		"io.k8s.apiextensions-apiserver.pkg.apis.apiextensions.v1.CustomResourceDefinitionList":      {},
		"io.k8s.apiextensions-apiserver.pkg.apis.apiextensions.v1.CustomResourceDefinitionNames":     {},
		"io.k8s.apiextensions-apiserver.pkg.apis.apiextensions.v1.CustomResourceDefinitionSpec":      {},
		"io.k8s.apiextensions-apiserver.pkg.apis.apiextensions.v1.CustomResourceDefinitionStatus":    {},
		"io.k8s.apiextensions-apiserver.pkg.apis.apiextensions.v1.CustomResourceDefinitionVersion":   {},
		"io.k8s.apiextensions-apiserver.pkg.apis.apiextensions.v1.CustomResourceSubresourceScale":    {},
		"io.k8s.apiextensions-apiserver.pkg.apis.apiextensions.v1.CustomResourceSubresourceStatus":   {},
		"io.k8s.apiextensions-apiserver.pkg.apis.apiextensions.v1.CustomResourceSubresources":        {},
		"io.k8s.apiextensions-apiserver.pkg.apis.apiextensions.v1.CustomResourceValidation":          {},
		"io.k8s.apiextensions-apiserver.pkg.apis.apiextensions.v1.ExternalDocumentation":             {},
		"io.k8s.apiextensions-apiserver.pkg.apis.apiextensions.v1.JSON":                              {},
		"io.k8s.apiextensions-apiserver.pkg.apis.apiextensions.v1.JSONSchemaProps":                   {},
		"io.k8s.apiextensions-apiserver.pkg.apis.apiextensions.v1.JSONSchemaPropsOrArray":            {},
		"io.k8s.apiextensions-apiserver.pkg.apis.apiextensions.v1.JSONSchemaPropsOrBool":             {},
		"io.k8s.apiextensions-apiserver.pkg.apis.apiextensions.v1.JSONSchemaPropsOrStringArray":      {},
		"io.k8s.apiextensions-apiserver.pkg.apis.apiextensions.v1.SelectableField":                   {},
		"io.k8s.apiextensions-apiserver.pkg.apis.apiextensions.v1.ServiceReference":                  {},
		"io.k8s.apiextensions-apiserver.pkg.apis.apiextensions.v1.ValidationRule":                    {},
		"io.k8s.apiextensions-apiserver.pkg.apis.apiextensions.v1.WebhookClientConfig":               {},
		"io.k8s.apiextensions-apiserver.pkg.apis.apiextensions.v1.WebhookConversion":                 {},
		"io.k8s.apimachinery.pkg.api.resource.Quantity":                                              {},
		"io.k8s.apimachinery.pkg.apis.meta.v1.APIGroup":                                              {},
		"io.k8s.apimachinery.pkg.apis.meta.v1.APIGroupList":                                          {},
		"io.k8s.apimachinery.pkg.apis.meta.v1.APIResource":                                           {},
		"io.k8s.apimachinery.pkg.apis.meta.v1.APIResourceList":                                       {},
		"io.k8s.apimachinery.pkg.apis.meta.v1.APIVersions":                                           {},
		"io.k8s.apimachinery.pkg.apis.meta.v1.Condition":                                             {},
		"io.k8s.apimachinery.pkg.apis.meta.v1.DeleteOptions":                                         {},
		"io.k8s.apimachinery.pkg.apis.meta.v1.FieldsV1":                                              {},
		"io.k8s.apimachinery.pkg.apis.meta.v1.GroupVersionForDiscovery":                              {},
		"io.k8s.apimachinery.pkg.apis.meta.v1.LabelSelector":                                         {},
		"io.k8s.apimachinery.pkg.apis.meta.v1.LabelSelectorRequirement":                              {},
		"io.k8s.apimachinery.pkg.apis.meta.v1.ListMeta":                                              {},
		"io.k8s.apimachinery.pkg.apis.meta.v1.ManagedFieldsEntry":                                    {},
		"io.k8s.apimachinery.pkg.apis.meta.v1.MicroTime":                                             {},
		"io.k8s.apimachinery.pkg.apis.meta.v1.ObjectMeta":                                            {},
		"io.k8s.apimachinery.pkg.apis.meta.v1.OwnerReference":                                        {},
		"io.k8s.apimachinery.pkg.apis.meta.v1.Patch":                                                 {},
		"io.k8s.apimachinery.pkg.apis.meta.v1.Preconditions":                                         {},
		"io.k8s.apimachinery.pkg.apis.meta.v1.ServerAddressByClientCIDR":                             {},
		"io.k8s.apimachinery.pkg.apis.meta.v1.Status":                                                {},
		"io.k8s.apimachinery.pkg.apis.meta.v1.StatusCause":                                           {},
		"io.k8s.apimachinery.pkg.apis.meta.v1.StatusDetails":                                         {},
		"io.k8s.apimachinery.pkg.apis.meta.v1.Time":                                                  {},
		"io.k8s.apimachinery.pkg.apis.meta.v1.WatchEvent":                                            {},
		"io.k8s.apimachinery.pkg.runtime.RawExtension":                                               {},
		"io.k8s.apimachinery.pkg.util.intstr.IntOrString":                                            {},
		"io.k8s.apimachinery.pkg.version.Info":                                                       {},
		"io.k8s.kube-aggregator.pkg.apis.apiregistration.v1.APIService":                              {},
		"io.k8s.kube-aggregator.pkg.apis.apiregistration.v1.APIServiceCondition":                     {},
		"io.k8s.kube-aggregator.pkg.apis.apiregistration.v1.APIServiceList":                          {},
		"io.k8s.kube-aggregator.pkg.apis.apiregistration.v1.APIServiceSpec":                          {},
		"io.k8s.kube-aggregator.pkg.apis.apiregistration.v1.APIServiceStatus":                        {},
		"io.k8s.kube-aggregator.pkg.apis.apiregistration.v1.ServiceReference":                        {},
	}
}

func getFilteredResourceArray() (res []string) {
	for val := range getFilteredResourceMap() {
		res = append(res, val)
	}

	return res
}

var nativeCmd = &cobra.Command{
	Use: "native",
	RunE: func(cmd *cobra.Command, args []string) error {
		start := time.Now()

		log, err := setupLogger("DEBUG")
		if err != nil {
			return err
		}

		log.Info().Str("LogLevel", log.GetLevel().String()).Msg("Starting server...")

		cfg := controllerruntime.GetConfigOrDie()

		runtimeClient, err := setupK8sClients(cfg)
		if err != nil {
			return err
		}

		resolver := native.NewResolver(log, runtimeClient)

		restMapper, err := getRestMapper(cfg)
		if err != nil {
			return fmt.Errorf("error getting rest mapper: %w", err)
		}

		definitions, filteredDefinitions := getDefinitionsAndFilteredDefinitions(log, cfg)
		g, err := native.New(log, restMapper, definitions, filteredDefinitions, resolver)
		if err != nil {
			return fmt.Errorf("error creating gateway: %w", err)
		}

		gqlSchema, err := g.GetGraphqlSchema()
		if err != nil {
			return fmt.Errorf("error creating GraphQL schema: %w", err)
		}

		http.Handle("/graphql", gateway.Handler(gateway.HandlerConfig{
			Config: &handler.Config{
				Schema:     &gqlSchema,
				Pretty:     true,
				Playground: true,
			},
			UserClaim:   "mail",
			GroupsClaim: "groups",
		}))

		log.Info().Float64("elapsed", time.Since(start).Seconds()).Msg("Setup took seconds")
		log.Info().Msg("Server is running on http://localhost:3000/graphql")

		return http.ListenAndServe(":3000", nil)
	},
}

func setupLogger(logLevel string) (*logger.Logger, error) {
	loggerCfg := logger.DefaultConfig()
	loggerCfg.Name = "gateway"
	loggerCfg.Level = logLevel
	return logger.New(loggerCfg)
}

// setupK8sClients initializes and returns the runtime client and cache for Kubernetes.
func setupK8sClients(cfg *rest.Config) (client.WithWatch, error) {
	if err := corev1.AddToScheme(scheme.Scheme); err != nil {
		return nil, fmt.Errorf("error adding core v1 to scheme: %w", err)
	}

	k8sCache, err := cache.New(cfg, cache.Options{Scheme: scheme.Scheme})
	if err != nil {
		return nil, err
	}

	go k8sCache.Start(context.Background())
	if !k8sCache.WaitForCacheSync(context.Background()) {
		return nil, fmt.Errorf("failed to sync cache")
	}

	runtimeClient, err := client.NewWithWatch(cfg, client.Options{
		Scheme: scheme.Scheme,
		Cache: &client.CacheOptions{
			Reader: k8sCache,
		},
	})

	return runtimeClient, err
}

// restMapper is needed to derive plural names for resources.
func getRestMapper(cfg *rest.Config) (meta.RESTMapper, error) {
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("error starting discovery client: %w", err)
	}

	groupResources, err := restmapper.GetAPIGroupResources(discoveryClient)
	if err != nil {
		log.Err(err).Msg("Error getting GetAPIGroupResources client")
		return nil, err
	}

	return restmapper.NewDiscoveryRESTMapper(groupResources), nil
}

// getDefinitionsAndFilteredDefinitions fetches OpenAPI schema definitions and filters resources.
func getDefinitionsAndFilteredDefinitions(log *logger.Logger, config *rest.Config) (spec.Definitions, spec.Definitions) {
	httpClient, err := rest.HTTPClientFor(config)
	if err != nil {
		panic(fmt.Sprintf("Error creating HTTP client: %v", err))
	}

	url := config.Host + "/openapi/v2"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		panic(fmt.Sprintf("Error creating request: %v", err))
	}

	if config.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+config.BearerToken)
	}

	resp, err := httpClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		panic(fmt.Sprintf("Error fetching OpenAPI schema: %v", err))
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		panic(fmt.Sprintf("Error reading response body: %v", err))
	}

	var swagger spec.Swagger
	if err := json.Unmarshal(body, &swagger); err != nil {
		panic(fmt.Sprintf("Error unmarshalling OpenAPI schema: %v", err))
	}

	err = expandSpec(false, log, &swagger, getFilteredResourceArray())

	filteredDefinitions := filterDefinitions(false, swagger.Definitions, getFilteredResourceMap())

	return swagger.Definitions, filteredDefinitions
}

// expandSpec expands the schema, it supports partial expand
func expandSpec(fullExpand bool, log *logger.Logger, swagger *spec.Swagger, targetDefinitions []string) error {
	// if fullExpand {
	// 	return spec.ExpandSpec(swagger, nil)
	// }
	//
	// for _, target := range targetDefinitions {
	// 	fmt.Println("### target", target)
	// 	if def, exists := swagger.Definitions[target]; exists {
	// 		fmt.Println("### ExpandSchema for", target)
	// 		err := spec.ExpandSchema(&def, &swagger, nil /* expandSpec options */)
	// 		if err != nil {
	// 			log.Error().Err(err).Str("target", target).Msg("Error expanding schema")
	// 			continue
	// 		}
	// 		// After expansion, reassign the expanded schema back
	// 		swagger.Definitions[target] = def
	// 	} else {
	// 		log.Warn().Str("target", target).Msg("definition not found in schema")
	// 	}
	// }
	return nil
}

// filterDefinitions filters definitions based on allowed resources.
func filterDefinitions(returnAll bool, definitions spec.Definitions, allowedResources map[string]struct{}) spec.Definitions {
	if returnAll {
		return definitions
	}

	filtered := make(map[string]spec.Schema)
	for key, val := range definitions {
		if _, ok := allowedResources[key]; ok {
			filtered[key] = val
		}
	}

	return filtered
}

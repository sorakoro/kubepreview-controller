/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// DeploymentRef references a source Deployment to duplicate.
type DeploymentRef struct {
	// name is the name of the source Deployment.
	// +required
	Name string `json:"name"`
}

// ServiceRef references a source Service to duplicate.
type ServiceRef struct {
	// name is the name of the source Service.
	// +required
	Name string `json:"name"`
}

// RoutingSpec defines the VirtualService routing configuration for the preview environment.
type RoutingSpec struct {
	// hosts is the list of hosts for the VirtualService (e.g., ["api-staging.wow.one"]).
	// +required
	Hosts []string `json:"hosts"`

	// gateways is the list of Istio gateways to attach to (e.g., ["wed-gateway"]).
	// +required
	Gateways []string `json:"gateways"`

	// headerName is the HTTP header name used for routing (e.g., "x-preview-env").
	// +optional
	// +kubebuilder:default="x-preview-env"
	HeaderName string `json:"headerName,omitempty"`

	// serviceName is the source Service name to route to the preview copy.
	// +required
	ServiceName string `json:"serviceName"`

	// port is the Service port to route to.
	// +optional
	// +kubebuilder:default=80
	Port int32 `json:"port,omitempty"`

	// namespace is the namespace where the VirtualService will be created.
	// If not set, uses the same namespace as the PreviewEnvironment.
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

// PreviewEnvironmentSpec defines the desired state of PreviewEnvironment
type PreviewEnvironmentSpec struct {
	// identifier is the suffix appended to duplicated resource names (e.g., "pr-10237").
	// +required
	Identifier string `json:"identifier"`

	// image is the container image to use for all duplicated Deployments.
	// +required
	Image string `json:"image"`

	// deployments is the list of source Deployments to duplicate.
	// +required
	Deployments []DeploymentRef `json:"deployments"`

	// services is the list of source Services to duplicate.
	// +optional
	Services []ServiceRef `json:"services,omitempty"`

	// replicas is the number of replicas for each duplicated Deployment. Defaults to 1.
	// +optional
	// +kubebuilder:default=1
	Replicas *int32 `json:"replicas,omitempty"`

	// ttl is the duration after which the PreviewEnvironment is automatically deleted (e.g., "72h").
	// +optional
	TTL *metav1.Duration `json:"ttl,omitempty"`

	// routing configures VirtualService-based header routing for the preview environment.
	// +optional
	Routing *RoutingSpec `json:"routing,omitempty"`
}

// DeploymentStatus represents the status of a duplicated Deployment.
type DeploymentStatus struct {
	// name is the name of the duplicated Deployment.
	Name string `json:"name"`
	// ready indicates whether the Deployment has available replicas.
	Ready bool `json:"ready"`
}

// ServiceStatus represents the status of a duplicated Service.
type ServiceStatus struct {
	// name is the name of the duplicated Service.
	Name string `json:"name"`
}

// PreviewEnvironmentStatus defines the observed state of PreviewEnvironment.
type PreviewEnvironmentStatus struct {
	// phase represents the current lifecycle phase.
	// +optional
	// +kubebuilder:validation:Enum=Pending;Provisioning;Ready;Error
	Phase string `json:"phase,omitempty"`

	// deployments is the status of each duplicated Deployment.
	// +optional
	Deployments []DeploymentStatus `json:"deployments,omitempty"`

	// services is the status of each duplicated Service.
	// +optional
	Services []ServiceStatus `json:"services,omitempty"`

	// virtualService is the name of the created VirtualService.
	// +optional
	VirtualService string `json:"virtualService,omitempty"`

	// expiresAt is the time when the PreviewEnvironment will be automatically deleted.
	// +optional
	ExpiresAt *metav1.Time `json:"expiresAt,omitempty"`

	// conditions represent the current state of the PreviewEnvironment resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// PreviewEnvironment is the Schema for the previewenvironments API
type PreviewEnvironment struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of PreviewEnvironment
	// +required
	Spec PreviewEnvironmentSpec `json:"spec"`

	// status defines the observed state of PreviewEnvironment
	// +optional
	Status PreviewEnvironmentStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// PreviewEnvironmentList contains a list of PreviewEnvironment
type PreviewEnvironmentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []PreviewEnvironment `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PreviewEnvironment{}, &PreviewEnvironmentList{})
}

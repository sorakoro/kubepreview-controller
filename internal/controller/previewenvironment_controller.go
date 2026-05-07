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

package controller

import (
	"context"
	"fmt"
	"time"

	networkingv1 "istio.io/api/networking/v1"
	istiov1 "istio.io/client-go/pkg/apis/networking/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	previewv1alpha1 "github.com/wedinc/kubepreview-controller/api/v1alpha1"
)

const (
	finalizerName = "preview.wow.one/cleanup"

	phaseProvisioning = "Provisioning"
	phaseReady        = "Ready"
	phaseError        = "Error"
)

// PreviewEnvironmentReconciler reconciles a PreviewEnvironment object
type PreviewEnvironmentReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=preview.wow.one,resources=previewenvironments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=preview.wow.one,resources=previewenvironments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=preview.wow.one,resources=previewenvironments/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.istio.io,resources=virtualservices,verbs=get;list;watch;create;update;patch;delete

func (r *PreviewEnvironmentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var pe previewv1alpha1.PreviewEnvironment
	if err := r.Get(ctx, req.NamespacedName, &pe); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Handle deletion
	if !pe.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(&pe, finalizerName) {
			log.Info("Cleaning up preview environment", "identifier", pe.Spec.Identifier)
			if err := r.cleanupResources(ctx, &pe); err != nil {
				return ctrl.Result{}, err
			}
			controllerutil.RemoveFinalizer(&pe, finalizerName)
			if err := r.Update(ctx, &pe); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// Add finalizer
	if !controllerutil.ContainsFinalizer(&pe, finalizerName) {
		controllerutil.AddFinalizer(&pe, finalizerName)
		if err := r.Update(ctx, &pe); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Check TTL expiry
	if pe.Spec.TTL != nil && pe.Status.ExpiresAt != nil {
		if time.Now().After(pe.Status.ExpiresAt.Time) {
			log.Info("PreviewEnvironment expired, deleting", "identifier", pe.Spec.Identifier)
			if err := r.Delete(ctx, &pe); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil
		}
	}

	// Set expiresAt if TTL is set and expiresAt is not
	if pe.Spec.TTL != nil && pe.Status.ExpiresAt == nil {
		expiresAt := metav1.NewTime(time.Now().Add(pe.Spec.TTL.Duration))
		pe.Status.ExpiresAt = &expiresAt
	}

	// Reconcile Deployments
	pe.Status.Phase = phaseProvisioning
	pe.Status.Deployments = nil
	allReady := true

	for _, depRef := range pe.Spec.Deployments {
		ready, err := r.reconcileDeployment(ctx, &pe, depRef.Name)
		if err != nil {
			log.Error(err, "Failed to reconcile deployment", "source", depRef.Name)
			pe.Status.Phase = phaseError
			allReady = false
			continue
		}
		previewName := fmt.Sprintf("%s-%s", depRef.Name, pe.Spec.Identifier)
		pe.Status.Deployments = append(pe.Status.Deployments, previewv1alpha1.DeploymentStatus{
			Name:  previewName,
			Ready: ready,
		})
		if !ready {
			allReady = false
		}
	}

	// Reconcile Services
	pe.Status.Services = nil
	for _, svcRef := range pe.Spec.Services {
		if err := r.reconcileService(ctx, &pe, svcRef.Name); err != nil {
			log.Error(err, "Failed to reconcile service", "source", svcRef.Name)
			pe.Status.Phase = phaseError
			continue
		}
		previewName := fmt.Sprintf("%s-%s", svcRef.Name, pe.Spec.Identifier)
		pe.Status.Services = append(pe.Status.Services, previewv1alpha1.ServiceStatus{
			Name: previewName,
		})
	}

	// Reconcile VirtualService
	if pe.Spec.Routing != nil {
		if err := r.reconcileVirtualService(ctx, &pe); err != nil {
			log.Error(err, "Failed to reconcile VirtualService")
			pe.Status.Phase = phaseError
		} else {
			vsName := fmt.Sprintf("%s-%s", pe.Spec.Routing.ServiceName, pe.Spec.Identifier)
			pe.Status.VirtualService = vsName
		}
	}

	if allReady && pe.Status.Phase != phaseError {
		pe.Status.Phase = phaseReady
	}

	// Update status
	if err := r.Status().Update(ctx, &pe); err != nil {
		return ctrl.Result{}, err
	}

	// Requeue for TTL
	if pe.Status.ExpiresAt != nil {
		remaining := time.Until(pe.Status.ExpiresAt.Time)
		if remaining > 0 {
			return ctrl.Result{RequeueAfter: remaining}, nil
		}
	}

	// Requeue if not ready
	if !allReady {
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	return ctrl.Result{}, nil
}

func (r *PreviewEnvironmentReconciler) reconcileDeployment(ctx context.Context, pe *previewv1alpha1.PreviewEnvironment, sourceName string) (bool, error) {
	// Get source Deployment
	var source appsv1.Deployment
	if err := r.Get(ctx, types.NamespacedName{Name: sourceName, Namespace: pe.Namespace}, &source); err != nil {
		return false, fmt.Errorf("source deployment %s not found: %w", sourceName, err)
	}

	previewName := fmt.Sprintf("%s-%s", sourceName, pe.Spec.Identifier)

	// Build the preview Deployment
	preview := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      previewName,
			Namespace: pe.Namespace,
		},
	}

	result, err := controllerutil.CreateOrUpdate(ctx, r.Client, preview, func() error {
		// Copy spec from source
		source.Spec.DeepCopyInto(&preview.Spec)

		// Rewrite labels and selectors
		rewriteLabels(preview.Spec.Selector.MatchLabels, sourceName, previewName)
		if preview.Spec.Template.Labels == nil {
			preview.Spec.Template.Labels = make(map[string]string)
		}
		rewriteLabels(preview.Spec.Template.Labels, sourceName, previewName)

		// Set preview labels on the Deployment itself
		if preview.Labels == nil {
			preview.Labels = make(map[string]string)
		}
		preview.Labels["preview.wow.one/identifier"] = pe.Spec.Identifier
		preview.Labels["preview.wow.one/source-deployment"] = sourceName

		// Replace all container images
		for i := range preview.Spec.Template.Spec.Containers {
			preview.Spec.Template.Spec.Containers[i].Image = pe.Spec.Image
		}

		// Set replicas
		if pe.Spec.Replicas != nil {
			preview.Spec.Replicas = pe.Spec.Replicas
		}

		// Set owner reference
		return controllerutil.SetControllerReference(pe, preview, r.Scheme)
	})
	if err != nil {
		return false, err
	}

	log := logf.FromContext(ctx)
	if result != controllerutil.OperationResultNone {
		log.Info("Deployment reconciled", "name", previewName, "operation", result)
	}

	// Check readiness
	return preview.Status.AvailableReplicas > 0, nil
}

func (r *PreviewEnvironmentReconciler) reconcileService(ctx context.Context, pe *previewv1alpha1.PreviewEnvironment, sourceName string) error {
	// Get source Service
	var source corev1.Service
	if err := r.Get(ctx, types.NamespacedName{Name: sourceName, Namespace: pe.Namespace}, &source); err != nil {
		return fmt.Errorf("source service %s not found: %w", sourceName, err)
	}

	previewName := fmt.Sprintf("%s-%s", sourceName, pe.Spec.Identifier)

	// Build the preview Service
	preview := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      previewName,
			Namespace: pe.Namespace,
		},
	}

	result, err := controllerutil.CreateOrUpdate(ctx, r.Client, preview, func() error {
		// Copy spec from source (preserve clusterIP/clusterIPs if already assigned)
		existingClusterIP := preview.Spec.ClusterIP
		existingClusterIPs := preview.Spec.ClusterIPs
		source.Spec.DeepCopyInto(&preview.Spec)
		if existingClusterIP != "" {
			preview.Spec.ClusterIP = existingClusterIP
			preview.Spec.ClusterIPs = existingClusterIPs
		} else {
			preview.Spec.ClusterIP = ""
			preview.Spec.ClusterIPs = nil
		}

		// Rewrite selector
		rewriteLabels(preview.Spec.Selector, sourceName, previewName)

		// Set preview labels
		if preview.Labels == nil {
			preview.Labels = make(map[string]string)
		}
		preview.Labels["preview.wow.one/identifier"] = pe.Spec.Identifier
		preview.Labels["preview.wow.one/source-service"] = sourceName

		// Set owner reference
		return controllerutil.SetControllerReference(pe, preview, r.Scheme)
	})
	if err != nil {
		return err
	}

	log := logf.FromContext(ctx)
	if result != controllerutil.OperationResultNone {
		log.Info("Service reconciled", "name", previewName, "operation", result)
	}

	return nil
}

func (r *PreviewEnvironmentReconciler) reconcileVirtualService(ctx context.Context, pe *previewv1alpha1.PreviewEnvironment) error {
	routing := pe.Spec.Routing
	previewServiceName := fmt.Sprintf("%s-%s", routing.ServiceName, pe.Spec.Identifier)
	vsName := fmt.Sprintf("%s-%s", routing.ServiceName, pe.Spec.Identifier)

	vsNamespace := pe.Namespace
	if routing.Namespace != "" {
		vsNamespace = routing.Namespace
	}

	// Build the destination host
	destHost := fmt.Sprintf("%s.%s.svc.cluster.local", previewServiceName, pe.Namespace)

	headerName := routing.HeaderName
	if headerName == "" {
		headerName = "x-preview-env"
	}

	port := routing.Port
	if port == 0 {
		port = 80
	}

	vs := &istiov1.VirtualService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      vsName,
			Namespace: vsNamespace,
		},
	}

	result, err := controllerutil.CreateOrUpdate(ctx, r.Client, vs, func() error {
		vs.Spec = networkingv1.VirtualService{
			Hosts:    routing.Hosts,
			Gateways: routing.Gateways,
			Http: []*networkingv1.HTTPRoute{
				{
					Match: []*networkingv1.HTTPMatchRequest{
						{
							Headers: map[string]*networkingv1.StringMatch{
								headerName: {
									MatchType: &networkingv1.StringMatch_Exact{
										Exact: pe.Spec.Identifier,
									},
								},
							},
						},
					},
					Route: []*networkingv1.HTTPRouteDestination{
						{
							Destination: &networkingv1.Destination{
								Host: destHost,
								Port: &networkingv1.PortSelector{
									Number: uint32(port),
								},
							},
						},
					},
				},
			},
		}

		// Set preview labels
		if vs.Labels == nil {
			vs.Labels = make(map[string]string)
		}
		vs.Labels["preview.wow.one/identifier"] = pe.Spec.Identifier

		// Set owner reference only if same namespace
		if vsNamespace == pe.Namespace {
			return controllerutil.SetControllerReference(pe, vs, r.Scheme)
		}
		return nil
	})
	if err != nil {
		return err
	}

	log := logf.FromContext(ctx)
	if result != controllerutil.OperationResultNone {
		log.Info("VirtualService reconciled", "name", vsName, "namespace", vsNamespace, "operation", result)
	}

	return nil
}

func (r *PreviewEnvironmentReconciler) cleanupResources(ctx context.Context, pe *previewv1alpha1.PreviewEnvironment) error {
	log := logf.FromContext(ctx)

	// Delete Deployments by label
	deploymentList := &appsv1.DeploymentList{}
	if err := r.List(ctx, deploymentList,
		client.InNamespace(pe.Namespace),
		client.MatchingLabels{"preview.wow.one/identifier": pe.Spec.Identifier},
	); err != nil {
		return err
	}
	for i := range deploymentList.Items {
		log.Info("Deleting deployment", "name", deploymentList.Items[i].Name)
		if err := r.Delete(ctx, &deploymentList.Items[i]); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}

	// Delete Services by label
	serviceList := &corev1.ServiceList{}
	if err := r.List(ctx, serviceList,
		client.InNamespace(pe.Namespace),
		client.MatchingLabels{"preview.wow.one/identifier": pe.Spec.Identifier},
	); err != nil {
		return err
	}
	for i := range serviceList.Items {
		log.Info("Deleting service", "name", serviceList.Items[i].Name)
		if err := r.Delete(ctx, &serviceList.Items[i]); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}

	// Delete VirtualServices by label
	if pe.Spec.Routing != nil {
		vsNamespace := pe.Namespace
		if pe.Spec.Routing.Namespace != "" {
			vsNamespace = pe.Spec.Routing.Namespace
		}
		vsList := &istiov1.VirtualServiceList{}
		if err := r.List(ctx, vsList,
			client.InNamespace(vsNamespace),
			client.MatchingLabels{"preview.wow.one/identifier": pe.Spec.Identifier},
		); err != nil {
			return err
		}
		for _, vs := range vsList.Items {
			log.Info("Deleting VirtualService", "name", vs.Name)
			if err := r.Delete(ctx, vs); err != nil && !apierrors.IsNotFound(err) {
				return err
			}
		}
	}

	return nil
}

// rewriteLabels replaces label values matching sourceName with previewName.
func rewriteLabels(labels map[string]string, sourceName, previewName string) {
	for k, v := range labels {
		if v == sourceName {
			labels[k] = previewName
		}
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *PreviewEnvironmentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&previewv1alpha1.PreviewEnvironment{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Named("previewenvironment").
		Complete(r)
}

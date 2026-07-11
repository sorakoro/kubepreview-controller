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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	istiov1 "istio.io/client-go/pkg/apis/networking/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	previewv1alpha1 "github.com/sorakoro/kubepreview-controller/api/v1alpha1"
)

const testNamespace = "default"

func int32Ptr(i int32) *int32 { v := new(int32); *v = i; return v }

func createSourceDeployment(ctx context.Context, name string) {
	labels := map[string]string{"app": name}
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "app", Image: "original:v1"},
					},
				},
			},
		},
	}
	Expect(k8sClient.Create(ctx, dep)).To(Succeed())
}

func createSourceService(ctx context.Context, name string) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": name},
			Ports: []corev1.ServicePort{
				{Port: 80, TargetPort: intstr.FromInt32(8080)},
			},
		},
	}
	Expect(k8sClient.Create(ctx, svc)).To(Succeed())
}

func reconcileOnce(ctx context.Context, name string) (reconcile.Result, error) {
	r := &PreviewEnvironmentReconciler{
		Client: k8sClient,
		Scheme: k8sClient.Scheme(),
	}
	return r.Reconcile(ctx, reconcile.Request{
		NamespacedName: types.NamespacedName{Name: name, Namespace: testNamespace},
	})
}

var _ = Describe("PreviewEnvironment Controller", func() {
	const (
		namespace  = testNamespace
		identifier = "pr-123"
		image      = "myapp:pr-123"
	)

	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	Context("Finalizer management", func() {
		const peName = "finalizer-test"

		BeforeEach(func() {
			createSourceDeployment(ctx, "web")
			pe := &previewv1alpha1.PreviewEnvironment{
				ObjectMeta: metav1.ObjectMeta{Name: peName, Namespace: namespace},
				Spec: previewv1alpha1.PreviewEnvironmentSpec{
					Identifier:  identifier,
					Image:       image,
					Deployments: []previewv1alpha1.DeploymentRef{{Name: "web"}},
				},
			}
			Expect(k8sClient.Create(ctx, pe)).To(Succeed())
		})

		AfterEach(func() {
			pe := &previewv1alpha1.PreviewEnvironment{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: peName, Namespace: namespace}, pe); err == nil {
				controllerutil.RemoveFinalizer(pe, finalizerName)
				_ = k8sClient.Update(ctx, pe)
				_ = k8sClient.Delete(ctx, pe)
			}
			dep := &appsv1.Deployment{}
			for _, name := range []string{"web", "web-" + identifier} {
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, dep); err == nil {
					_ = k8sClient.Delete(ctx, dep)
				}
			}
		})

		It("adds finalizer on first reconcile", func() {
			_, err := reconcileOnce(ctx, peName)
			Expect(err).NotTo(HaveOccurred())

			pe := &previewv1alpha1.PreviewEnvironment{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: peName, Namespace: namespace}, pe)).To(Succeed())
			Expect(controllerutil.ContainsFinalizer(pe, finalizerName)).To(BeTrue())
		})
	})

	Context("Deployment reconciliation", func() {
		const peName = "deploy-test"

		BeforeEach(func() {
			createSourceDeployment(ctx, "api")
		})

		AfterEach(func() {
			pe := &previewv1alpha1.PreviewEnvironment{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: peName, Namespace: namespace}, pe); err == nil {
				controllerutil.RemoveFinalizer(pe, finalizerName)
				_ = k8sClient.Update(ctx, pe)
				_ = k8sClient.Delete(ctx, pe)
			}
			for _, name := range []string{"api", "api-" + identifier} {
				dep := &appsv1.Deployment{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, dep); err == nil {
					_ = k8sClient.Delete(ctx, dep)
				}
			}
		})

		It("creates a preview Deployment with correct labels, image, and owner reference", func() {
			pe := &previewv1alpha1.PreviewEnvironment{
				ObjectMeta: metav1.ObjectMeta{Name: peName, Namespace: namespace},
				Spec: previewv1alpha1.PreviewEnvironmentSpec{
					Identifier:  identifier,
					Image:       image,
					Deployments: []previewv1alpha1.DeploymentRef{{Name: "api"}},
				},
			}
			Expect(k8sClient.Create(ctx, pe)).To(Succeed())

			// First reconcile adds finalizer
			_, err := reconcileOnce(ctx, peName)
			Expect(err).NotTo(HaveOccurred())
			// Second reconcile creates resources
			_, err = reconcileOnce(ctx, peName)
			Expect(err).NotTo(HaveOccurred())

			previewName := fmt.Sprintf("api-%s", identifier)
			dep := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: previewName, Namespace: namespace}, dep)).To(Succeed())

			// Labels rewritten
			Expect(dep.Spec.Selector.MatchLabels["app"]).To(Equal(previewName))
			Expect(dep.Spec.Template.Labels["app"]).To(Equal(previewName))

			// Preview labels set
			Expect(dep.Labels["preview.kubepreview.dev/identifier"]).To(Equal(identifier))
			Expect(dep.Labels["preview.kubepreview.dev/source-deployment"]).To(Equal("api"))

			// Image replaced
			Expect(dep.Spec.Template.Spec.Containers[0].Image).To(Equal(image))

			// Owner reference set
			Expect(dep.OwnerReferences).To(HaveLen(1))
			Expect(dep.OwnerReferences[0].Name).To(Equal(peName))
		})

		It("sets replicas from the PE spec", func() {
			pe := &previewv1alpha1.PreviewEnvironment{
				ObjectMeta: metav1.ObjectMeta{Name: peName, Namespace: namespace},
				Spec: previewv1alpha1.PreviewEnvironmentSpec{
					Identifier:  identifier,
					Image:       image,
					Deployments: []previewv1alpha1.DeploymentRef{{Name: "api"}},
					Replicas:    int32Ptr(3),
				},
			}
			Expect(k8sClient.Create(ctx, pe)).To(Succeed())

			_, _ = reconcileOnce(ctx, peName)
			_, _ = reconcileOnce(ctx, peName)

			previewName := fmt.Sprintf("api-%s", identifier)
			dep := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: previewName, Namespace: namespace}, dep)).To(Succeed())
			Expect(*dep.Spec.Replicas).To(Equal(int32(3)))
		})

		It("sets phase to Error when source Deployment is missing", func() {
			pe := &previewv1alpha1.PreviewEnvironment{
				ObjectMeta: metav1.ObjectMeta{Name: peName, Namespace: namespace},
				Spec: previewv1alpha1.PreviewEnvironmentSpec{
					Identifier:  identifier,
					Image:       image,
					Deployments: []previewv1alpha1.DeploymentRef{{Name: "nonexistent"}},
				},
			}
			Expect(k8sClient.Create(ctx, pe)).To(Succeed())

			_, _ = reconcileOnce(ctx, peName)
			_, _ = reconcileOnce(ctx, peName)

			updated := &previewv1alpha1.PreviewEnvironment{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: peName, Namespace: namespace}, updated)).To(Succeed())
			Expect(updated.Status.Phase).To(Equal(phaseError))
		})
	})

	Context("Service reconciliation", func() {
		const peName = "svc-test"

		BeforeEach(func() {
			createSourceDeployment(ctx, "backend")
			createSourceService(ctx, "backend")
		})

		AfterEach(func() {
			pe := &previewv1alpha1.PreviewEnvironment{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: peName, Namespace: namespace}, pe); err == nil {
				controllerutil.RemoveFinalizer(pe, finalizerName)
				_ = k8sClient.Update(ctx, pe)
				_ = k8sClient.Delete(ctx, pe)
			}
			for _, name := range []string{"backend", "backend-" + identifier} {
				dep := &appsv1.Deployment{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, dep); err == nil {
					_ = k8sClient.Delete(ctx, dep)
				}
				svc := &corev1.Service{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, svc); err == nil {
					_ = k8sClient.Delete(ctx, svc)
				}
			}
		})

		It("creates a preview Service with rewritten selector and preview labels", func() {
			pe := &previewv1alpha1.PreviewEnvironment{
				ObjectMeta: metav1.ObjectMeta{Name: peName, Namespace: namespace},
				Spec: previewv1alpha1.PreviewEnvironmentSpec{
					Identifier:  identifier,
					Image:       image,
					Deployments: []previewv1alpha1.DeploymentRef{{Name: "backend"}},
					Services:    []previewv1alpha1.ServiceRef{{Name: "backend"}},
				},
			}
			Expect(k8sClient.Create(ctx, pe)).To(Succeed())

			_, _ = reconcileOnce(ctx, peName)
			_, _ = reconcileOnce(ctx, peName)

			previewName := fmt.Sprintf("backend-%s", identifier)
			svc := &corev1.Service{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: previewName, Namespace: namespace}, svc)).To(Succeed())

			// Selector rewritten
			Expect(svc.Spec.Selector["app"]).To(Equal(previewName))

			// Preview labels set
			Expect(svc.Labels["preview.kubepreview.dev/identifier"]).To(Equal(identifier))
			Expect(svc.Labels["preview.kubepreview.dev/source-service"]).To(Equal("backend"))

			// Owner reference set
			Expect(svc.OwnerReferences).To(HaveLen(1))
		})

		It("sets phase to Error when source Service is missing", func() {
			pe := &previewv1alpha1.PreviewEnvironment{
				ObjectMeta: metav1.ObjectMeta{Name: peName, Namespace: namespace},
				Spec: previewv1alpha1.PreviewEnvironmentSpec{
					Identifier:  identifier,
					Image:       image,
					Deployments: []previewv1alpha1.DeploymentRef{{Name: "backend"}},
					Services:    []previewv1alpha1.ServiceRef{{Name: "no-such-svc"}},
				},
			}
			Expect(k8sClient.Create(ctx, pe)).To(Succeed())

			_, _ = reconcileOnce(ctx, peName)
			_, _ = reconcileOnce(ctx, peName)

			updated := &previewv1alpha1.PreviewEnvironment{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: peName, Namespace: namespace}, updated)).To(Succeed())
			Expect(updated.Status.Phase).To(Equal(phaseError))
		})
	})

	Context("VirtualService reconciliation", func() {
		const peName = "vs-test"

		BeforeEach(func() {
			createSourceDeployment(ctx, "frontend")
			createSourceService(ctx, "frontend")
		})

		AfterEach(func() {
			pe := &previewv1alpha1.PreviewEnvironment{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: peName, Namespace: namespace}, pe); err == nil {
				controllerutil.RemoveFinalizer(pe, finalizerName)
				_ = k8sClient.Update(ctx, pe)
				_ = k8sClient.Delete(ctx, pe)
			}
			for _, name := range []string{"frontend", "frontend-" + identifier} {
				dep := &appsv1.Deployment{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, dep); err == nil {
					_ = k8sClient.Delete(ctx, dep)
				}
				svc := &corev1.Service{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, svc); err == nil {
					_ = k8sClient.Delete(ctx, svc)
				}
			}
			vsName := fmt.Sprintf("frontend-%s", identifier)
			vs := &istiov1.VirtualService{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: vsName, Namespace: namespace}, vs); err == nil {
				_ = k8sClient.Delete(ctx, vs)
			}
		})

		It("creates a VirtualService with header-based routing", func() {
			pe := &previewv1alpha1.PreviewEnvironment{
				ObjectMeta: metav1.ObjectMeta{Name: peName, Namespace: namespace},
				Spec: previewv1alpha1.PreviewEnvironmentSpec{
					Identifier:  identifier,
					Image:       image,
					Deployments: []previewv1alpha1.DeploymentRef{{Name: "frontend"}},
					Services:    []previewv1alpha1.ServiceRef{{Name: "frontend"}},
					Routing: &previewv1alpha1.RoutingSpec{
						Hosts:       []string{"app.example.com"},
						Gateways:    []string{"my-gateway"},
						ServiceName: "frontend",
						Port:        8080,
						HeaderName:  "x-preview-id",
					},
				},
			}
			Expect(k8sClient.Create(ctx, pe)).To(Succeed())

			_, _ = reconcileOnce(ctx, peName)
			_, _ = reconcileOnce(ctx, peName)

			vsName := fmt.Sprintf("frontend-%s", identifier)
			vs := &istiov1.VirtualService{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vsName, Namespace: namespace}, vs)).To(Succeed())

			// Hosts and gateways
			Expect(vs.Spec.Hosts).To(Equal([]string{"app.example.com"}))
			Expect(vs.Spec.Gateways).To(Equal([]string{"my-gateway"}))

			// Header match
			Expect(vs.Spec.Http).To(HaveLen(1))
			Expect(vs.Spec.Http[0].Match).To(HaveLen(1))
			headerMatch := vs.Spec.Http[0].Match[0].Headers["x-preview-id"]
			Expect(headerMatch).NotTo(BeNil())
			Expect(headerMatch.GetExact()).To(Equal(identifier))

			// Route destination
			expectedHost := fmt.Sprintf("frontend-%s.default.svc.cluster.local", identifier)
			Expect(vs.Spec.Http[0].Route[0].Destination.Host).To(Equal(expectedHost))
			Expect(vs.Spec.Http[0].Route[0].Destination.Port.Number).To(Equal(uint32(8080)))

			// Preview label
			Expect(vs.Labels["preview.kubepreview.dev/identifier"]).To(Equal(identifier))

			// Status
			updated := &previewv1alpha1.PreviewEnvironment{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: peName, Namespace: namespace}, updated)).To(Succeed())
			Expect(updated.Status.VirtualService).To(Equal(vsName))
		})

		It("uses default header name and port when not specified", func() {
			pe := &previewv1alpha1.PreviewEnvironment{
				ObjectMeta: metav1.ObjectMeta{Name: peName, Namespace: namespace},
				Spec: previewv1alpha1.PreviewEnvironmentSpec{
					Identifier:  identifier,
					Image:       image,
					Deployments: []previewv1alpha1.DeploymentRef{{Name: "frontend"}},
					Routing: &previewv1alpha1.RoutingSpec{
						Hosts:       []string{"app.example.com"},
						Gateways:    []string{"my-gateway"},
						ServiceName: "frontend",
					},
				},
			}
			Expect(k8sClient.Create(ctx, pe)).To(Succeed())

			_, _ = reconcileOnce(ctx, peName)
			_, _ = reconcileOnce(ctx, peName)

			vsName := fmt.Sprintf("frontend-%s", identifier)
			vs := &istiov1.VirtualService{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vsName, Namespace: namespace}, vs)).To(Succeed())

			// Default header name
			headerMatch := vs.Spec.Http[0].Match[0].Headers["x-preview-env"]
			Expect(headerMatch).NotTo(BeNil())

			// Default port
			Expect(vs.Spec.Http[0].Route[0].Destination.Port.Number).To(Equal(uint32(80)))
		})

		It("does not create VirtualService when routing is nil", func() {
			pe := &previewv1alpha1.PreviewEnvironment{
				ObjectMeta: metav1.ObjectMeta{Name: peName, Namespace: namespace},
				Spec: previewv1alpha1.PreviewEnvironmentSpec{
					Identifier:  identifier,
					Image:       image,
					Deployments: []previewv1alpha1.DeploymentRef{{Name: "frontend"}},
				},
			}
			Expect(k8sClient.Create(ctx, pe)).To(Succeed())

			_, _ = reconcileOnce(ctx, peName)
			_, _ = reconcileOnce(ctx, peName)

			vsName := fmt.Sprintf("frontend-%s", identifier)
			vs := &istiov1.VirtualService{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: vsName, Namespace: namespace}, vs)
			Expect(errors.IsNotFound(err)).To(BeTrue())

			// Status should not have virtualService set
			updated := &previewv1alpha1.PreviewEnvironment{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: peName, Namespace: namespace}, updated)).To(Succeed())
			Expect(updated.Status.VirtualService).To(BeEmpty())
		})
	})

	Context("Status updates", func() {
		const peName = "status-test"

		BeforeEach(func() {
			createSourceDeployment(ctx, "worker")
			createSourceService(ctx, "worker")
		})

		AfterEach(func() {
			pe := &previewv1alpha1.PreviewEnvironment{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: peName, Namespace: namespace}, pe); err == nil {
				controllerutil.RemoveFinalizer(pe, finalizerName)
				_ = k8sClient.Update(ctx, pe)
				_ = k8sClient.Delete(ctx, pe)
			}
			for _, name := range []string{"worker", "worker-" + identifier} {
				dep := &appsv1.Deployment{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, dep); err == nil {
					_ = k8sClient.Delete(ctx, dep)
				}
				svc := &corev1.Service{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, svc); err == nil {
					_ = k8sClient.Delete(ctx, svc)
				}
			}
		})

		It("populates deployment and service status", func() {
			pe := &previewv1alpha1.PreviewEnvironment{
				ObjectMeta: metav1.ObjectMeta{Name: peName, Namespace: namespace},
				Spec: previewv1alpha1.PreviewEnvironmentSpec{
					Identifier:  identifier,
					Image:       image,
					Deployments: []previewv1alpha1.DeploymentRef{{Name: "worker"}},
					Services:    []previewv1alpha1.ServiceRef{{Name: "worker"}},
				},
			}
			Expect(k8sClient.Create(ctx, pe)).To(Succeed())

			_, _ = reconcileOnce(ctx, peName)
			_, _ = reconcileOnce(ctx, peName)

			updated := &previewv1alpha1.PreviewEnvironment{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: peName, Namespace: namespace}, updated)).To(Succeed())

			previewName := fmt.Sprintf("worker-%s", identifier)
			Expect(updated.Status.Deployments).To(HaveLen(1))
			Expect(updated.Status.Deployments[0].Name).To(Equal(previewName))

			Expect(updated.Status.Services).To(HaveLen(1))
			Expect(updated.Status.Services[0].Name).To(Equal(previewName))
		})

		It("transitions to Ready when deployments are available", func() {
			pe := &previewv1alpha1.PreviewEnvironment{
				ObjectMeta: metav1.ObjectMeta{Name: peName, Namespace: namespace},
				Spec: previewv1alpha1.PreviewEnvironmentSpec{
					Identifier:  identifier,
					Image:       image,
					Deployments: []previewv1alpha1.DeploymentRef{{Name: "worker"}},
				},
			}
			Expect(k8sClient.Create(ctx, pe)).To(Succeed())

			// Reconcile to create the preview Deployment
			_, _ = reconcileOnce(ctx, peName)
			_, _ = reconcileOnce(ctx, peName)

			// Simulate readiness by updating the preview Deployment's status
			previewName := fmt.Sprintf("worker-%s", identifier)
			dep := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: previewName, Namespace: namespace}, dep)).To(Succeed())
			dep.Status.AvailableReplicas = 1
			dep.Status.ReadyReplicas = 1
			dep.Status.Replicas = 1
			Expect(k8sClient.Status().Update(ctx, dep)).To(Succeed())

			// Reconcile again
			_, _ = reconcileOnce(ctx, peName)

			updated := &previewv1alpha1.PreviewEnvironment{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: peName, Namespace: namespace}, updated)).To(Succeed())
			Expect(updated.Status.Phase).To(Equal(phaseReady))
			Expect(updated.Status.Deployments[0].Ready).To(BeTrue())
		})

		It("requeues after 10s when deployments are not ready", func() {
			pe := &previewv1alpha1.PreviewEnvironment{
				ObjectMeta: metav1.ObjectMeta{Name: peName, Namespace: namespace},
				Spec: previewv1alpha1.PreviewEnvironmentSpec{
					Identifier:  identifier,
					Image:       image,
					Deployments: []previewv1alpha1.DeploymentRef{{Name: "worker"}},
				},
			}
			Expect(k8sClient.Create(ctx, pe)).To(Succeed())

			_, _ = reconcileOnce(ctx, peName)
			result, err := reconcileOnce(ctx, peName)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(10 * time.Second))
		})
	})

	Context("TTL and auto-deletion", func() {
		const peName = "ttl-test"

		BeforeEach(func() {
			createSourceDeployment(ctx, "temp")
		})

		AfterEach(func() {
			pe := &previewv1alpha1.PreviewEnvironment{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: peName, Namespace: namespace}, pe); err == nil {
				controllerutil.RemoveFinalizer(pe, finalizerName)
				_ = k8sClient.Update(ctx, pe)
				_ = k8sClient.Delete(ctx, pe)
			}
			for _, name := range []string{"temp", "temp-" + identifier} {
				dep := &appsv1.Deployment{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, dep); err == nil {
					_ = k8sClient.Delete(ctx, dep)
				}
			}
		})

		It("sets expiresAt when TTL is specified", func() {
			ttl := metav1.Duration{Duration: 1 * time.Hour}
			pe := &previewv1alpha1.PreviewEnvironment{
				ObjectMeta: metav1.ObjectMeta{Name: peName, Namespace: namespace},
				Spec: previewv1alpha1.PreviewEnvironmentSpec{
					Identifier:  identifier,
					Image:       image,
					Deployments: []previewv1alpha1.DeploymentRef{{Name: "temp"}},
					TTL:         &ttl,
				},
			}
			Expect(k8sClient.Create(ctx, pe)).To(Succeed())

			_, _ = reconcileOnce(ctx, peName)
			_, _ = reconcileOnce(ctx, peName)

			updated := &previewv1alpha1.PreviewEnvironment{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: peName, Namespace: namespace}, updated)).To(Succeed())
			Expect(updated.Status.ExpiresAt).NotTo(BeNil())
			// expiresAt should be approximately 1 hour from now
			Expect(updated.Status.ExpiresAt.Time).To(BeTemporally("~", time.Now().Add(1*time.Hour), 5*time.Second))
		})

		It("deletes the PE when expiresAt is in the past", func() {
			ttl := metav1.Duration{Duration: 1 * time.Hour}
			pe := &previewv1alpha1.PreviewEnvironment{
				ObjectMeta: metav1.ObjectMeta{Name: peName, Namespace: namespace},
				Spec: previewv1alpha1.PreviewEnvironmentSpec{
					Identifier:  identifier,
					Image:       image,
					Deployments: []previewv1alpha1.DeploymentRef{{Name: "temp"}},
					TTL:         &ttl,
				},
			}
			Expect(k8sClient.Create(ctx, pe)).To(Succeed())

			// Reconcile to set expiresAt
			_, _ = reconcileOnce(ctx, peName)
			_, _ = reconcileOnce(ctx, peName)

			// Manually set expiresAt to the past
			updated := &previewv1alpha1.PreviewEnvironment{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: peName, Namespace: namespace}, updated)).To(Succeed())
			pastTime := metav1.NewTime(time.Now().Add(-1 * time.Hour))
			updated.Status.ExpiresAt = &pastTime
			Expect(k8sClient.Status().Update(ctx, updated)).To(Succeed())

			// Reconcile should delete the PE
			_, err := reconcileOnce(ctx, peName)
			Expect(err).NotTo(HaveOccurred())

			// PE should be deleted (or have deletionTimestamp set)
			result := &previewv1alpha1.PreviewEnvironment{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: peName, Namespace: namespace}, result)
			if err == nil {
				// If still exists, it should have a deletion timestamp
				Expect(result.DeletionTimestamp).NotTo(BeNil())
			} else {
				Expect(errors.IsNotFound(err)).To(BeTrue())
			}
		})
	})

	Context("Deletion and cleanup", func() {
		const peName = "cleanup-test"

		BeforeEach(func() {
			createSourceDeployment(ctx, "cleanup-app")
			createSourceService(ctx, "cleanup-app")
		})

		AfterEach(func() {
			for _, name := range []string{"cleanup-app", "cleanup-app-" + identifier} {
				dep := &appsv1.Deployment{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, dep); err == nil {
					_ = k8sClient.Delete(ctx, dep)
				}
				svc := &corev1.Service{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, svc); err == nil {
					_ = k8sClient.Delete(ctx, svc)
				}
			}
		})

		It("cleans up preview resources and removes finalizer on deletion", func() {
			pe := &previewv1alpha1.PreviewEnvironment{
				ObjectMeta: metav1.ObjectMeta{Name: peName, Namespace: namespace},
				Spec: previewv1alpha1.PreviewEnvironmentSpec{
					Identifier:  identifier,
					Image:       image,
					Deployments: []previewv1alpha1.DeploymentRef{{Name: "cleanup-app"}},
					Services:    []previewv1alpha1.ServiceRef{{Name: "cleanup-app"}},
				},
			}
			Expect(k8sClient.Create(ctx, pe)).To(Succeed())

			// Reconcile to create resources
			_, _ = reconcileOnce(ctx, peName)
			_, _ = reconcileOnce(ctx, peName)

			// Verify preview resources exist
			previewName := fmt.Sprintf("cleanup-app-%s", identifier)
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: previewName, Namespace: namespace}, &appsv1.Deployment{})).To(Succeed())
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: previewName, Namespace: namespace}, &corev1.Service{})).To(Succeed())

			// Delete the PE
			current := &previewv1alpha1.PreviewEnvironment{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: peName, Namespace: namespace}, current)).To(Succeed())
			Expect(k8sClient.Delete(ctx, current)).To(Succeed())

			// Reconcile to trigger cleanup
			_, err := reconcileOnce(ctx, peName)
			Expect(err).NotTo(HaveOccurred())

			// Preview Deployment should be cleaned up
			depList := &appsv1.DeploymentList{}
			Expect(k8sClient.List(ctx, depList,
				client.InNamespace(namespace),
				client.MatchingLabels{"preview.kubepreview.dev/identifier": identifier},
			)).To(Succeed())
			Expect(depList.Items).To(BeEmpty())

			// Preview Service should be cleaned up
			svcList := &corev1.ServiceList{}
			Expect(k8sClient.List(ctx, svcList,
				client.InNamespace(namespace),
				client.MatchingLabels{"preview.kubepreview.dev/identifier": identifier},
			)).To(Succeed())
			Expect(svcList.Items).To(BeEmpty())

			// PE should be gone (finalizer removed)
			err = k8sClient.Get(ctx, types.NamespacedName{Name: peName, Namespace: namespace}, &previewv1alpha1.PreviewEnvironment{})
			Expect(errors.IsNotFound(err)).To(BeTrue())
		})
	})
})

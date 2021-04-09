// Copyright (c) 2021 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project.
package observabilityendpoint

import (
	"context"
	"strings"
	"testing"

	fakeconfigclient "github.com/openshift/client-go/config/clientset/versioned/fake"
	appv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	addonv1alpha1 "github.com/open-cluster-management/api/addon/v1alpha1"
	oashared "github.com/open-cluster-management/multicluster-observability-operator/api/shared"
	oav1beta1 "github.com/open-cluster-management/multicluster-observability-operator/api/v1beta1"
)

const (
	name            = "observability-addon"
	testNamespace   = "test-ns"
	testHubNamspace = "test-hub-ns"
	hubInfoName     = "hub-info-secret"
	podName         = "metrics-collector-deployment-abc-xyz"
)

func newObservabilityAddon(name string, ns string) *oav1beta1.ObservabilityAddon {
	return &oav1beta1.ObservabilityAddon{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: oashared.ObservabilityAddonSpec{
			EnableMetrics: true,
			Interval:      60,
		},
	}
}

func newPod() *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: namespace,
			Labels: map[string]string{
				selectorKey: selectorValue,
			},
		},
	}
}

func newPromSvc() *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      promSvcName,
			Namespace: promNamespace,
		},
	}
}

func newHubInfoSecret() *corev1.Secret {
	data := []byte(`
cluster-name: "test-cluster"
endpoint: "http://test-endpoint"
`)
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      hubConfigName,
			Namespace: testNamespace,
		},
		Data: map[string][]byte{
			hubInfoKey: data,
		},
	}
}

func init() {
	s := scheme.Scheme
	addonv1alpha1.AddToScheme(s)
	oav1beta1.AddToScheme(s)

	namespace = testNamespace
	hubNamespace = testHubNamspace
}

func TestObservabilityAddonController(t *testing.T) {

	hubObjs := []runtime.Object{}
	hubInfo := newHubInfoSecret()
	allowList := getAllowlistCM()
	objs := []runtime.Object{hubInfo, allowList}

	hubClient := fake.NewFakeClient(hubObjs...)
	ocpClient := fakeconfigclient.NewSimpleClientset(cv)
	c := fake.NewFakeClient(objs...)

	r := &ObservabilityAddonReconciler{
		Client:    c,
		HubClient: hubClient,
		OcpClient: ocpClient,
	}

	// test error in reconcile if missing obervabilityaddon
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "install",
			Namespace: testNamespace,
		},
	}
	ctx := context.TODO()
	_, err := r.Reconcile(ctx, req)
	if err == nil {
		t.Fatalf("reconcile: miss the error for missing obervabilityaddon")
	}

	// test reconcile w/o prometheus-k8s svc
	err = hubClient.Create(ctx, newObservabilityAddon(name, testHubNamspace))
	if err != nil {
		t.Fatalf("failed to create hub oba to install: (%v)", err)
	}
	oba := newObservabilityAddon(name, testNamespace)
	err = c.Create(ctx, oba)
	if err != nil {
		t.Fatalf("failed to create oba to install: (%v)", err)
	}
	req = ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "install",
			Namespace: testNamespace,
		},
	}
	_, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	// test reconcile successfully with all resources installed and finalizer set
	promSvc := newPromSvc()
	err = c.Create(ctx, promSvc)
	if err != nil {
		t.Fatalf("failed to create prom svc to install: (%v)", err)
	}
	req = ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "install",
			Namespace: testNamespace,
		},
	}
	_, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}
	rb := &rbacv1.ClusterRoleBinding{}
	err = c.Get(ctx, types.NamespacedName{Name: clusterRoleBindingName,
		Namespace: ""}, rb)
	if err != nil {
		t.Fatalf("Required clusterrolebinding not created: (%v)", err)
	}
	cm := &corev1.ConfigMap{}
	err = c.Get(ctx, types.NamespacedName{Name: caConfigmapName,
		Namespace: namespace}, cm)
	if err != nil {
		t.Fatalf("Required configmap not created: (%v)", err)
	}
	deploy := &appv1.Deployment{}
	err = c.Get(ctx, types.NamespacedName{Name: metricsCollectorName,
		Namespace: namespace}, deploy)
	if err != nil {
		t.Fatalf("Metrics collector deployment not created: (%v)", err)
	}
	foundOba := &oav1beta1.ObservabilityAddon{}
	err = hubClient.Get(ctx, types.NamespacedName{Name: obAddonName,
		Namespace: hubNamespace}, foundOba)
	if err != nil {
		t.Fatalf("Failed to get observabilityAddon: (%v)", err)
	}
	if !contains(foundOba.Finalizers, obsAddonFinalizer) {
		t.Fatal("Finalizer not set in observabilityAddon")
	}

	// test reconcile w/o clusterversion(OCP 3.11)
	r.OcpClient = fakeconfigclient.NewSimpleClientset()
	req = ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "install",
			Namespace: testNamespace,
		},
	}
	_, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}
	err = c.Get(ctx, types.NamespacedName{Name: metricsCollectorName,
		Namespace: namespace}, deploy)
	if err != nil {
		t.Fatalf("Metrics collector deployment not created: (%v)", err)
	}
	commands := deploy.Spec.Template.Spec.Containers[0].Command
	for _, cmd := range commands {
		if strings.Contains(cmd, "clusterID=") && !strings.Contains(cmd, "test-cluster") {
			t.Fatalf("Found wrong clusterID in command: (%s)", cmd)
		}
	}

	// test reconcile metrics collector pod deleted if cert secret updated
	err = c.Create(ctx, newPod())
	if err != nil {
		t.Fatalf("Failed to create pod: (%v)", err)
	}
	req = ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      mtlsCertName,
			Namespace: testNamespace,
		},
	}
	_, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile for update: (%v)", err)
	}
	pod := &corev1.Pod{}
	err = c.Get(ctx, types.NamespacedName{Name: podName,
		Namespace: namespace}, pod)
	if !errors.IsNotFound(err) {
		t.Fatal("Pod not deleted")
	}

	// test reconcile  metrics collector's replicas set to 0 if observability disabled
	err = c.Delete(ctx, oba)
	if err != nil {
		t.Fatalf("failed to delete obsaddon to disable: (%v)", err)
	}
	oba = newObservabilityAddon(name, testNamespace)
	oba.Spec.EnableMetrics = false
	err = c.Create(ctx, oba)
	if err != nil {
		t.Fatalf("failed to create obsaddon to disable: (%v)", err)
	}
	req = ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "disable",
			Namespace: testNamespace,
		},
	}
	_, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile for disable: (%v)", err)
	}
	err = c.Get(ctx, types.NamespacedName{Name: metricsCollectorName,
		Namespace: namespace}, deploy)
	if err != nil {
		t.Fatalf("Metrics collector deployment not created: (%v)", err)
	}
	if *deploy.Spec.Replicas != 0 {
		t.Fatalf("Replicas for metrics collector deployment is not set as 0, value is (%d)", *deploy.Spec.Replicas)
	}

	// test reconcile all resources and finalizer are removed
	err = c.Delete(ctx, oba)
	if err != nil {
		t.Fatalf("failed to delete obsaddon to delete: (%v)", err)
	}
	req = ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "delete",
			Namespace: testNamespace,
		},
	}
	_, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile for delete: (%v)", err)
	}
	err = c.Get(ctx, types.NamespacedName{Name: clusterRoleBindingName,
		Namespace: ""}, rb)
	if !errors.IsNotFound(err) {
		t.Fatalf("Required clusterrolebinding not deleted")
	}
	err = c.Get(ctx, types.NamespacedName{Name: caConfigmapName,
		Namespace: namespace}, cm)
	if !errors.IsNotFound(err) {
		t.Fatalf("Required configmap not deleted")
	}
	err = c.Get(ctx, types.NamespacedName{Name: metricsCollectorName,
		Namespace: namespace}, deploy)
	if !errors.IsNotFound(err) {
		t.Fatalf("Metrics collector deployment not deleted")
	}
	foundOba1 := &oav1beta1.ObservabilityAddon{}
	err = hubClient.Get(ctx, types.NamespacedName{Name: obAddonName,
		Namespace: hubNamespace}, foundOba1)
	if err != nil {
		t.Fatalf("Failed to get observabilityAddon: (%v)", err)
	}
	if contains(foundOba1.Finalizers, obsAddonFinalizer) {
		t.Fatal("Finalizer not removed from observabilityAddon")
	}
}
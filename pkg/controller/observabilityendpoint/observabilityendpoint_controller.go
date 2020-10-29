// Copyright (c) 2020 Red Hat, Inc.

package observabilityendpoint

import (
	"context"
	"os"

	ocpClientSet "github.com/openshift/client-go/config/clientset/versioned"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	addonv1alpha1 "github.com/open-cluster-management/api/addon/v1alpha1"
	oav1beta1 "github.com/open-cluster-management/multicluster-monitoring-operator/pkg/apis/observability/v1beta1"
)

const (
	hubConfigName           = "hub-info-secret"
	obAddonName             = "observability-addon"
	mcoCRName               = "observability"
	ownerLabelKey           = "owner"
	ownerLabelValue         = "multicluster-operator"
	epFinalizer             = "observability.open-cluster-management.io/addon-cleanup"
	managedClusterAddonName = "observability-controller"
	promSvcName             = "prometheus-k8s"
	promNamespace           = "openshift-monitoring"
)

var (
	namespace    = os.Getenv("NAMESPACE")
	hubNamespace = os.Getenv("WATCH_NAMESPACE")
	log          = logf.Log.WithName("controller_observabilityaddon")
)

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

// Add creates a new ObservabilityAddon Controller and adds it to the Manager.
// The Manager will set fields on the Controller and Start it when the Manager
// is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	// Create kube client
	kubeClient, err := createKubeClient()
	if err != nil {
		log.Error(err, "Failed to create the Kubernetes client")
		return nil
	}
	// Create OCP client
	ocpClient, err := createOCPClient()
	if err != nil {
		log.Error(err, "Failed to create the OpenShift client")
		return nil
	}
	return &ReconcileObservabilityAddon{
		client:     mgr.GetClient(),
		kubeClient: kubeClient,
		ocpClient:  ocpClient,
		scheme:     mgr.GetScheme()}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("endpointmonitoring-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	pred := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			if e.Meta.GetName() == obAddonName && e.Meta.GetAnnotations()[ownerLabelKey] == ownerLabelValue {
				return true
			}
			return false
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			if e.Meta.GetName() == obAddonName && e.Meta.GetAnnotations()[ownerLabelKey] == ownerLabelValue {
				return !e.DeleteStateUnknown
			}
			return false
		},
	}

	mcoPred := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			if e.Meta.GetName() == mcoCRName {
				return true
			}
			return false
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			if e.MetaNew.GetName() == mcoCRName {
				return true
			}
			return false
		},
	}

	// Watch for changes to primary resource ObservabilityAddon
	err = c.Watch(&source.Kind{Type: &oav1beta1.ObservabilityAddon{}}, &handler.EnqueueRequestForObject{}, pred)
	if err != nil {
		return err
	}

	// Watch for changes to primary resource MCO CR
	err = c.Watch(&source.Kind{Type: &oav1beta1.MultiClusterObservability{}}, &handler.EnqueueRequestForObject{}, mcoPred)
	if err != nil {
		return err
	}

	return nil
}

// blank assignment to verify that ReconcileObservabilityAddon implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileObservabilityAddon{}

// ReconcileObservabilityAddon reconciles a ObservabilityAddon object
type ReconcileObservabilityAddon struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client     client.Client
	scheme     *runtime.Scheme
	kubeClient kubernetes.Interface
	ocpClient  ocpClientSet.Interface
}

// Reconcile reads that state of the cluster for a ObservabilityAddon object and makes changes based on the state read
// and what is in the ObservabilityAddon.Spec
// Modify this Reconcile function to implement your Controller logic.  This example creates
// a Pod as an example
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileObservabilityAddon) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling ObservabilityAddon")

	// Fetch the ObservabilityAddon instance
	instance := &oav1beta1.ObservabilityAddon{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: obAddonName, Namespace: hubNamespace}, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	// Init finalizers
	deleted, err := r.initFinalization(instance)
	if err != nil {
		return reconcile.Result{}, err
	}
	if deleted {
		return reconcile.Result{}, nil
	}

	// Fetch the ManagedClusterAddon instance
	mcaInstance := &addonv1alpha1.ManagedClusterAddOn{}
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: managedClusterAddonName,
		Namespace: hubNamespace}, mcaInstance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			reqLogger.Info("Cannot find ManagedClusterAddOn ", managedClusterAddonName, "namespace ", hubNamespace)
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	mcoInstance := &oav1beta1.MultiClusterObservability{}
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: mcoCRName, Namespace: ""}, mcoInstance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			reqLogger.Info("Cannot find mco observability")
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	// If no prometheus service found, set status as NotSupported
	_, err = r.kubeClient.CoreV1().Services(promNamespace).Get(context.TODO(), promSvcName, metav1.GetOptions{})
	if err != nil {
		reqLogger.Error(err, "Failed to get prometheus resource")
		reportStatus(r.client, instance, "NotSupported")
		reportStatusToMCAddon(r.client, mcaInstance, "NotSupported")
		return reconcile.Result{}, nil
	}
	// hubSecret is in ManifestWork, Read from local k8s client
	// ocp_resource.go
	//	err = r.client.Get(context.TODO(), types.NamespacedName{Name: hubConfigName, Namespace: request.Namespace}, hubSecret)
	hubSecret, err := r.kubeClient.CoreV1().Secrets(namespace).Get(context.TODO(), hubConfigName, metav1.GetOptions{}) //(context.TODO(), types.NamespacedName{Name: hubConfigName, Namespace: request.Namespace}, hubSecret)
	if err != nil {
		reqLogger.Error(err, "Failed to get hub secret")
		return reconcile.Result{}, err
	}
	clusterID, err := getClusterID(r.ocpClient)
	if err != nil {
		// OCP 3.11 has no cluster id, set it as empty string
		clusterID = ""
	}

	err = createMonitoringClusterRoleBinding(r.kubeClient)
	if err != nil {
		return reconcile.Result{}, err
	}
	err = createCAConfigmap(r.kubeClient)
	if err != nil {
		return reconcile.Result{}, err
	}

	if mcoInstance.Spec.ObservabilityAddonSpec.EnableMetrics {
		created, err := updateMetricsCollector(r.kubeClient, hubSecret, clusterID, *mcoInstance.Spec.ObservabilityAddonSpec, 1)
		if err != nil {
			reportStatusToMCAddon(r.client, mcaInstance, "Degraded")
			return reconcile.Result{}, err
		}
		if created {
			reportStatus(r.client, instance, "Ready")
			reportStatusToMCAddon(r.client, mcaInstance, "Ready")
		}
	} else {
		deleted, err := updateMetricsCollector(r.kubeClient, hubSecret, clusterID, *mcoInstance.Spec.ObservabilityAddonSpec, 0)
		if err != nil {
			return reconcile.Result{}, err
		}
		if deleted {
			reportStatus(r.client, instance, "Disabled")
			reportStatusToMCAddon(r.client, mcaInstance, "Disabled")
		}
	}

	//TODO: UPDATE
	return reconcile.Result{}, nil
}

func (r *ReconcileObservabilityAddon) initFinalization(
	ep *oav1beta1.ObservabilityAddon) (bool, error) {
	if ep.GetDeletionTimestamp() != nil && contains(ep.GetFinalizers(), epFinalizer) {
		log.Info("To revert configurations")
		err := deleteMetricsCollector(r.kubeClient)
		if err != nil {
			return false, err
		}
		// Should we return bool from the delete functions for crb and cm? What is it used for? Should we use the bool before removing finalizer?
		//SHould we return true if metricscollector is not found as that means  metrics collector is not present?
		//Moved this part up as we need to clean up cm and crb before we remove the finalizer - is that the right way to do it?
		err = deleteMonitoringClusterRoleBinding(r.kubeClient)
		if err != nil {
			return false, err
		}
		err = deleteCAConfigmap(r.kubeClient)
		if err != nil {
			return false, err
		}
		ep.SetFinalizers(remove(ep.GetFinalizers(), epFinalizer))
		err = r.client.Update(context.TODO(), ep)
		if err != nil {
			log.Error(err, "Failed to remove finalizer to endpointmonitoring", "namespace", ep.Namespace)
			return false, err
		}
		log.Info("Finalizer removed from endpointmonitoring resource")
		return true, nil
	}
	if !contains(ep.GetFinalizers(), epFinalizer) {
		ep.SetFinalizers(append(ep.GetFinalizers(), epFinalizer))
		err := r.client.Update(context.TODO(), ep)
		if err != nil {
			log.Error(err, "Failed to add finalizer to endpointmonitoring", "namespace", ep.Namespace)
			return false, err
		}
		log.Info("Finalizer added to endpointmonitoring resource")
	}
	return false, nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func remove(list []string, s string) []string {
	result := []string{}
	for _, v := range list {
		if v != s {
			result = append(result, v)
		}
	}
	return result
}

func createOCPClient() (ocpClientSet.Interface, error) {
	// create the config from the path
	config, err := clientcmd.BuildConfigFromFlags("", "")
	if err != nil {
		log.Error(err, "Failed to create the config")
		return nil, err
	}

	// generate the client based off of the config
	ocpClient, err := ocpClientSet.NewForConfig(config)
	if err != nil {
		log.Error(err, "Failed to create ocp config client")
		return nil, err
	}

	return ocpClient, err
}

func createKubeClient() (kubernetes.Interface, error) {
	// create the config from the path
	config, err := clientcmd.BuildConfigFromFlags("", "")
	if err != nil {
		log.Error(err, "Failed to create the config")
		return nil, err
	}

	// generate the client based off of the config
	kubeClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Error(err, "Failed to create kube client")
		return nil, err
	}

	return kubeClient, err
}

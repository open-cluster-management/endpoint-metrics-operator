// Copyright (c) 2020 Red Hat, Inc.

package observabilityendpoint

import (
	ocpClientSet "github.com/openshift/client-go/config/clientset/versioned"
	v1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	clusterRoleBindingName = "metrics-collector-view"
	caConfigmapName        = "metrics-collector-serving-certs-ca-bundle"
)

func createMonitoringClusterRoleBinding(client kubernetes.Interface) error {
	_, err := client.RbacV1().ClusterRoleBindings().Get(clusterRoleBindingName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			rb := &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterRoleBindingName,
					Namespace: namespace,
					Annotations: map[string]string{
						ownerLabelKey: ownerLabelValue,
					},
				},
				RoleRef: rbacv1.RoleRef{
					Kind:     "ClusterRole",
					Name:     "cluster-monitoring-view",
					APIGroup: "rbac.authorization.k8s.io",
				},
				Subjects: []rbacv1.Subject{
					{
						Kind:      "ServiceAccount",
						Name:      "default",
						Namespace: namespace,
					},
				},
			}
			_, err = client.RbacV1().ClusterRoleBindings().Create(rb)
			if err == nil {
				log.Info("clusterrolebinding created")
			} else {
				log.Error(err, "Failed to create the clusterrolebinding")
			}
			return err
		}
		log.Error(err, "Failed to check the clusterrolebinding")
		return err
	} else {
		log.Info("The clusterrolebinding already existed")
	}
	return nil
}

func createCAConfigmap(client kubernetes.Interface) error {
	_, err := client.CoreV1().ConfigMaps(namespace).Get(caConfigmapName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			cm := &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      caConfigmapName,
					Namespace: namespace,
					Annotations: map[string]string{
						ownerLabelKey: ownerLabelValue,
						"service.alpha.openshift.io/inject-cabundle": "true",
					},
				},
				Data: map[string]string{"service-ca.crt": ""},
			}
			_, err = client.CoreV1().ConfigMaps(namespace).Create(cm)
			if err == nil {
				log.Info("Configmap created")
			} else {
				log.Error(err, "Failed to create the configmap")
			}
			return err
		} else {
			log.Error(err, "Failed to check the configmap")
			return err
		}
	} else {
		log.Info("The configmap already existed")
	}
	return nil
}

// getClusterID is used to get the cluster uid
func getClusterID(ocpClient ocpClientSet.Interface) (string, error) {
	clusterVersion, err := ocpClient.ConfigV1().ClusterVersions().Get("version", metav1.GetOptions{})
	if err != nil {
		log.Error(err, "Failed to get clusterVersion")
		return "", err
	}

	return string(clusterVersion.Spec.ClusterID), nil
}
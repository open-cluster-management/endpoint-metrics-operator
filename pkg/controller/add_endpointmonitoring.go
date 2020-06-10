package controller

import (
	"github.com/open-cluster-management/endpoint-metrics-operator/pkg/controller/endpointmonitoring"
)

func init() {
	// AddToManagerFuncs is a list of functions to create controllers and add them to a manager.
	AddToManagerFuncs = append(AddToManagerFuncs, endpointmonitoring.Add)
}
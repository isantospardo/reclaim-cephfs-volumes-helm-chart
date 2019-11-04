package main

import (
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Declare a kubeclient as a global var to be accessible by all the functions
// when running in a pod, this will automatically use the pod's serviceaccount to access the cluster API
var kubeclient = Kubeclient{kubeclient: NewKubeClient()}

type Kubeclient struct {
	kubeclient *kubernetes.Clientset
}

// Creates a client
func NewKubeClient() *kubernetes.Clientset {

	// creates the in-cluster config
	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err.Error())
	}
	// creates the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}
	return clientset
}

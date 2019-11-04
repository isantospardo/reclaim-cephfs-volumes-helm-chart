package main

import (
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"
)

// Sets annotations to the Persistent Volume
func setPVDateAnnotation(pvName, annotationKey string, date time.Time) error {
	// use the same RFC3339 date format as Kubernetes already uses for all date representation on resources.
	patch := []byte(fmt.Sprintf(`{"metadata": {"annotations": {"%s": "%s"}}}`, annotationKey, date.Format(time.RFC3339)))
	_, err := kubeclient.kubeclient.CoreV1().PersistentVolumes().Patch(pvName, types.StrategicMergePatchType, patch)
	if err != nil {
		klog.Errorf("ERROR: patching annotation PV %s", err)
		return err
	}
	return nil
}

// Patch reclaim policy of the PV
func patchPVReclaimingPolicy(pvName, policy string) error {
	patch := []byte(fmt.Sprintf(`{"spec": {"persistentVolumeReclaimPolicy": "%s"}}`, policy))
	_, err := kubeclient.kubeclient.CoreV1().PersistentVolumes().Patch(pvName, types.StrategicMergePatchType, patch)
	if err != nil {
		klog.Errorf("ERROR: patching reclaim policy PV %s", err)
		return err
	}
	return nil
}

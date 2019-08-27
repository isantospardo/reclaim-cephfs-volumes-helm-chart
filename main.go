package main

import (
	"flag"
	"fmt"
	"strings"
	"time"

	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"
)

const (
	annotationTimeReclaimVolumes = "time-reclaim-volumes.cern.ch/time-reclaim"
	annotationReclaimPolicy      = "persistentVolumeReclaimPolicy"
	annotationDelete             = "volume-ready-to-delete.cern.ch/delete-volume"
	reclaimPolicy                = "Delete"
)

var (
	tActual                 = time.Now()
	tParsingStorageClass    time.Duration
	tSelectedToDeleteVolume = "720h"

	// Register values for command-line flag parsing, by default we add a value
	// In order to interact with this, it is only required to add the pertinent flag during the execution (e.g. -reclaimTimePV 5h)
	loopCreationTimePVPtr = flag.String("loopCreationTimePV", "-1h", "Sets the max time loop for volumes created to be removed")
	storageClassNamePtr   = flag.String("storageClassName", "cephfs", "Specify the storageClassName")
)

func main() {

	// Initializing global flags for klog
	klog.InitFlags(nil)

	// Called it to parse the command line into the defined flags
	flag.Parse()

	// Get global kubeclient
	clientset := kubeclient.kubeclient

	// List all persistent volumes
	pvList, err := clientset.CoreV1().PersistentVolumes().List(meta_v1.ListOptions{})
	if err != nil {
		klog.Fatalf("ERROR: Impossible to retrieve the list of all persistent volumes %s ", err)
	}

	// Loop over all the PVs
	for _, persV := range pvList.Items {

		// Get annotation from the storageClass of the PV in order to get the timeReclaimVolume annotation, if there is no an existing storageClass,
		// the cronjob will run and sets tSelectedToDeleteVolume to a default value
		storageClassObj, err := clientset.StorageV1beta1().StorageClasses().Get(persV.Spec.StorageClassName, meta_v1.GetOptions{})
		if err != nil {
			klog.Infof("INFO: StorageClass of volume %s does not exists %s \n", persV.GetName(), err)
		}

		// Check whether the annotationTimeReclaimVolumes is set set to the StorageClass and if not set tSelectedToDeleteVolume to 720h (30d)
		if _, ok := storageClassObj.ObjectMeta.Annotations[annotationTimeReclaimVolumes]; ok {
			tSelectedToDeleteVolume = storageClassObj.ObjectMeta.Annotations[annotationTimeReclaimVolumes]
		}

		// Parse time by StorageClass annotation if it exists, if not parse the default tSelectedToDeleteVolume
		tParsingStorageClass, err = time.ParseDuration(tSelectedToDeleteVolume)
		if err != nil {
			klog.Errorf("ERROR: Impossible to parse time %s ", err)
			continue
		}

		// Calculates when the PV is going to be deleted, tActual + tSelectedToDeleteVolume
		tFuture := tActual.Add(tParsingStorageClass)

		// Parse time loopCreationTimePVPtr,
		// which will subtract the time specified by default will be -1h
		tActualSubs, err := time.ParseDuration(*loopCreationTimePVPtr)
		if err != nil {
			klog.Errorf("ERROR: Impossible to parse time %s ", err)
			continue
		}
		tPast := tActual.Add(tActualSubs)

		if persV.Status.Phase == "Released" {

			// Check weather the storageClass and the storageClassName set in the PV are equivalent
			if persV.Spec.StorageClassName == *storageClassNamePtr && persV.Spec.StorageClassName == storageClassObj.ObjectMeta.Name {

				klog.Infoln("INFO: PersistentVolume: ", persV.GetName(), " with Storage: ", *storageClassNamePtr, " - Status: ", persV.Status.Phase)

				// If annotation has not been set yet to delete the PV, set annotation to be considered during the next run
				if _, ok := persV.ObjectMeta.Annotations[annotationDelete]; !ok {
					// If the volume has been deleted before loopCreationTimePVPtr (default to 1h) from the creation time, it will be automatically deleted,
					// this will prevent issues like the one we had in OTG0048218 creating PVs in a loop
					if persV.GetCreationTimestamp().Sub(tPast) > 0 {
						klog.Infof("INFO: PV '%s' was marked for deletion less than '%s' ago. Proceeding with the deletion ...", persV.GetName(), strings.Replace(*loopCreationTimePVPtr, "-", "", -1))

						// Set annotation to delete the persistentVolume instantly
						err = patchPV(persV.Name, annotationReclaimPolicy, reclaimPolicy)
						if err != nil {
							klog.Errorf("INFO: PV '%s' reclaimPolicy set to %s!", persV.Name, reclaimPolicy)
							continue
						}
					} else {
						// When the PV has been marked as deleted after the first hour from the creation has past, an annotation will be
						// set in order to delete the volume in tSelectedToDeleteVolume
						klog.Infof("INFO: The PV %s will be deleted into %v \n", persV.Name, tFuture)
						err = setAnnotationsPV(persV.Name, annotationDelete, fmt.Sprintf("%s", tFuture))
						if err != nil {
							klog.Errorf("ERROR: patching PV %s with annotation %s %v", persV.Name, annotationDelete, tFuture)
							continue
						}
					}
					// When the PV has already been marked for deletion and the actual time is higher than the time annotated, it will delete the PV
				} else if tActual.String() >= persV.ObjectMeta.Annotations[annotationDelete] {
					klog.Infof("PV '%s' is marked for deletion on the '%s', which has already passed. Proceeding with the deletion ...", persV.Name, persV.ObjectMeta.Annotations[annotationDelete])
					err = patchPV(persV.Name, annotationReclaimPolicy, reclaimPolicy)
					if err != nil {
						klog.Errorf("ERROR: patching PV %s with annotation %s %s", persV.Name, annotationReclaimPolicy, reclaimPolicy)
						continue
					}
					klog.Infof("INFO: PV '%s' reclaimPolicy set to %s!", persV.Name, reclaimPolicy)
				}
			}
		}
	}
	klog.Infof("All existing PersistentVolumes have been processed")
}

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
	annotationPeriodReclaimVolumesAfterRelease = "reclaim-volumes.cern.ch/deletion-grace-period-after-release"
	annotationNoGracePeriodSinceCreation       = "reclaim-volumes.cern.ch/no-grace-period-if-time-since-creation-is-less-than"
	annotationReclaimPolicy                    = "persistentVolumeReclaimPolicy"
	annotationDelete                           = "volume-ready-to-delete.cern.ch/delete-volume"
	reclaimPolicy                              = "Delete"
)

var (
	tNow                       = time.Now()
	tParsingPVAnnotation       time.Duration
	noGracePeriodSinceCreation string
	// Register value for command-line flag parsing, by default we add a value
	// In order to interact with this, it is only required to add the pertinent flag during the execution (e.g. -storageClassName cephfs)
	storageClassNamePtr = flag.String("storageClassName", "cephfs", "Specify the storageClassName")
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

		// Check whether the annotationPeriodReclaimVolumesAfterRelease is set in the annotations of the PV
		if _, ok := persV.ObjectMeta.Annotations[annotationPeriodReclaimVolumesAfterRelease]; ok {
			tParsingPVAnnotation, err = time.ParseDuration(persV.ObjectMeta.Annotations[annotationPeriodReclaimVolumesAfterRelease])
			if err != nil {
				klog.Errorf("ERROR: Impossible to parse time %s volume: %s ", err, persV.Name)
				continue
			}
		} else {
			continue
		}

		// Calculates when the PV is going to be deleted, tNow + annotationPeriodReclaimVolumesAfterRelease formatted into RFC3339
		tFutureDeletionPV := tNow.Add(tParsingPVAnnotation).Format(time.RFC3339)

		// Parse time noGracePeriodSinceCreation from annotation if exists, this string has to be with negative sign
		if _, ok := persV.ObjectMeta.Annotations[annotationNoGracePeriodSinceCreation]; ok {
			noGracePeriodSinceCreation = persV.ObjectMeta.Annotations[annotationNoGracePeriodSinceCreation]
		} else {
			continue
		}

		tNowSubs, err := time.ParseDuration(noGracePeriodSinceCreation)
		if err != nil {
			klog.Errorf("ERROR: Impossible to parse time %s volume: %s ", err, persV.Name)
			continue
		}
		// tNowSubs has to be in negative as it is the easiest way to subtract duration from time.Time
		tPast := tNow.Add(-tNowSubs)

		if persV.Status.Phase != "Released" {
			//nothing to do
			continue
		} else {
			// Check whether the storageClass and the storageClassName set in the PV are equivalent
			if persV.Spec.StorageClassName == *storageClassNamePtr {

				klog.Infoln("INFO: PersistentVolume: ", persV.GetName(), " with Storage: ", *storageClassNamePtr, " - Status: ", persV.Status.Phase)

				// If annotation is not set to delete the PV, set annotation to be considered during the next run
				if _, ok := persV.ObjectMeta.Annotations[annotationDelete]; !ok {

					// When the PV is marked as "Released" after noGracePeriodSinceCreation has passed from the creation has past, a PV annotation
					// is set in order to delete the volume in annotationPeriodReclaimVolumesAfterRelease.
					if persV.GetCreationTimestamp().Sub(tPast) < 0 {
						klog.Infof("INFO: The PV %s will be deleted into %v \n", persV.Name, tFutureDeletionPV)
						err = setAnnotationsPV(persV.Name, annotationDelete, fmt.Sprintf("%s", tFutureDeletionPV))
						if err != nil {
							klog.Errorf("ERROR: patching PV %s with annotation %s %v", persV.Name, annotationDelete, tFutureDeletionPV)
							continue
						}
						// If the volume is marked as "Released" before noGracePeriodSinceCreation has passed from the creation time, it will be automatically deleted.
						// This will prevent issues like OTG0048218, creating PVs in a loop
					} else {
						klog.Infof("INFO: PV '%s' was marked for deletion less than '%s' ago. Proceeding with the deletion ...", persV.GetName(), strings.Replace(noGracePeriodSinceCreation, "-", "", -1))

						// Set annotation to delete the persistentVolume instantly
						err = patchPV(persV.Name, annotationReclaimPolicy, reclaimPolicy)
						if err != nil {
							klog.Errorf("INFO: PV '%s' reclaimPolicy set to %s!", persV.Name, reclaimPolicy)
							continue
						}
					}
					// When the PV is marked for deletion, and the actual time is higher than the time annotated, it will delete the PV
				} else {
					// Parse annotation to delete into time.RFC3339 format
					tDeleteParsed, err := time.Parse("2006-01-02T15:04:05+01:00", persV.ObjectMeta.Annotations[annotationDelete])
					if err != nil {
						klog.Errorf("ERROR: parsing annotation %s in PV %s %v", annotationDelete, persV.Name, err)
						continue
					}

					// Delete PVs where tNow is higher than the annotationDelete of the PV
					if tNow.After(tDeleteParsed) {

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
	}
	klog.Infof("All existing PersistentVolumes have been processed")
}

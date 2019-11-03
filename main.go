package main

import (
	"flag"
	"time"
	"fmt"

	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/api/core/v1"
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


func pvIsMarkedForDeletion(persV v1.PersistentVolume) bool{
	// Check whether the annotationPeriodReclaimVolumesAfterRelease is set in the annotations of the PV
	if _, ok := persV.ObjectMeta.Annotations[annotationPeriodReclaimVolumesAfterRelease]; ok {
		_, err := time.ParseDuration(persV.ObjectMeta.Annotations[annotationPeriodReclaimVolumesAfterRelease])
		if err != nil {
			klog.Errorf("ERROR: Impossible to parse time %s volume: %s ", err, persV.Name)
			return false
		}
		return true
	}
	return false
}

func timeToDeleteHasPassed(persV v1.PersistentVolume) bool {

	if _, ok := persV.ObjectMeta.Annotations[annotationDelete]; ok {
		// Parse annotation to delete into time.RFC3339 format
		tDeleteParsed, err := time.Parse("2006-01-02T15:04:05Z07:00", persV.ObjectMeta.Annotations[annotationDelete])
		if err != nil {
			klog.Errorf("ERROR: parsing annotation %s in PV %s %v", annotationDelete, persV.Name, err)
			return false
		}
		// Delete PVs where tNow is higher than the annotationDelete of the PV
		if tNow.After(tDeleteParsed) {
			klog.Infof("PV '%s' is marked for deletion on the '%s', which has already passed", persV.Name, persV.ObjectMeta.Annotations[annotationDelete])
			return true
		}
		return false
	}
	return false
}

func deletePV(persV v1.PersistentVolume) error{
	err := patchPV(persV.Name, annotationReclaimPolicy, reclaimPolicy)
	if err != nil {
		klog.Errorf("ERROR: patching PV %s with annotation %s %s", persV.Name, annotationReclaimPolicy, reclaimPolicy)
		return err
	}
	klog.Infof("INFO: PV '%s' reclaimPolicy set to %s!", persV.Name, reclaimPolicy)
	return nil 
}


func getTimeDeletion(persV v1.PersistentVolume) (tFutureDeletionPV string, tPast time.Time, err error){

	tParsingPVAnnotation, err = time.ParseDuration(persV.ObjectMeta.Annotations[annotationPeriodReclaimVolumesAfterRelease])
	tFutureDeletionPV = tNow.Add(tParsingPVAnnotation).Format(time.RFC3339)
	noGracePeriodSinceCreation := persV.ObjectMeta.Annotations[annotationNoGracePeriodSinceCreation]
	tNowSubs, err := time.ParseDuration(noGracePeriodSinceCreation)
	if err != nil {
		klog.Errorf("ERROR: Impossible to parse time %s volume: %s ", err, persV.Name)
		return
	}
	// tNowSubs has to be in negative as it is the easiest way to subtract duration from time.Time
	tPast = tNow.Add(-tNowSubs)
	return
}


func setAnnotationPVToDelete(persV v1.PersistentVolume) {

	tFutureDeletionPV, tPast, err := getTimeDeletion(persV)	
	if err != nil{
		klog.Errorf("ERROR: Impossible to getTimeDeletion %s volume: %s ", err, persV.Name)
		return
	}

	// When the PV is marked as "Released" after noGracePeriodSinceCreation has passed from the creation has past, a PV annotation
	// is set in order to delete the volume in annotationPeriodReclaimVolumesAfterRelease.
	if persV.GetCreationTimestamp().Sub(tPast) < 0 {
		klog.Infof("INFO: The PV %s will be deleted into %v \n", persV.Name, tFutureDeletionPV)
		err := setAnnotationsPV(persV.Name, annotationDelete, fmt.Sprintf("%s", tFutureDeletionPV))
		if err != nil {
			klog.Errorf("ERROR: patching PV %s with annotation %s %v", persV.Name, annotationDelete, tFutureDeletionPV)
			return
		}
	} else{
		klog.Infof("INFO: PersistentVolume: %s is in a loop creation", persV.GetName())
		deletePV(persV)
	}
}

func pvIsLoopCreation(persV v1.PersistentVolume) bool {

	tFutureDeletionPV, tPast, err := getTimeDeletion(persV)
	if err != nil{
		klog.Errorf("ERROR: Impossible to getTimeDeletion %s volume: %s ", err, persV.Name)
		return false
	}
	if persV.GetCreationTimestamp().Sub(tPast) < 0 {
		klog.Infof("INFO: The PV %s will be deleted into %v \n", persV.Name, tFutureDeletionPV)
		return true
	}
	return false
}

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
		//fmt.Println(reflect.TypeOf(persV))

		if persV.Spec.StorageClassName == *storageClassNamePtr {
			if persV.Status.Phase == "Released" {
				klog.Infoln("INFO: PersistentVolume: ", persV.GetName(), " with Storage: ", *storageClassNamePtr, " - Status: ", persV.Status.Phase)

				if pvIsMarkedForDeletion(persV) && timeToDeleteHasPassed(persV){
					klog.Infof("INFO: PersistentVolume: %s is being deleted", persV.Name)
					deletePV(persV)
				}

				// if pvIsLoopCreation(persV){
				// 	klog.Infof("INFO: PersistentVolume: %s is in a loop creation", persV.GetName())
				// 	deletePV(persV)
				// }

				if _, ok := persV.ObjectMeta.Annotations[annotationDelete]; !ok {
					klog.Infof("INFO: PersistentVolume: %s does not have annotation", persV.Name)
					setAnnotationPVToDelete(persV)

					
				}
			}
		}
	}
	klog.Infof("All existing PersistentVolumes have been processed")
}
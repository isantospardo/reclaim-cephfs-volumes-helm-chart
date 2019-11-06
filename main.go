package main

import (
	"flag"
	"time"

	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/api/core/v1"
	"k8s.io/klog"
)

const (
	annotationPeriodReclaimVolumesAfterRelease = "reclaim-volumes.cern.ch/deletion-grace-period-after-release"
	annotationNoGracePeriodSinceCreation       = "reclaim-volumes.cern.ch/no-grace-period-if-time-since-creation-is-less-than"
	annotationReclaimPolicy                    = "persistentVolumeReclaimPolicy"
	annotationDelete                           = "reclaim-volumes.cern.ch/volume-reclaim-deletion-timestamp"
)

func pvGracePeriodHasExpired(persV v1.PersistentVolume) bool {
	// Only consider the annotationDelete annotation here. We do not check whether annotationPeriodReclaimVolumesAfterRelease is also set.
	// This means other workflows or manual action can set annotationDelete independently of that annotation being set by this program
	// based on  annotationPeriodReclaimVolumesAfterRelease.
	tDeleteParsed, err := time.Parse(time.RFC3339, persV.ObjectMeta.Annotations[annotationDelete])
	if err != nil {
		return false
	}
	// Delete PVs where tNow is higher than the annotationDelete of the PV
	if time.Now().After(tDeleteParsed) {
		klog.Infof("PV '%s' is marked for deletion on the '%s', which has already passed", persV.Name, persV.ObjectMeta.Annotations[annotationDelete])
		return true
	}
	return false
}

func requestPVDeletion(persV v1.PersistentVolume) {
	reclaimPolicy := "Delete"
	if err := patchPVReclaimingPolicy(persV.Name, reclaimPolicy); err == nil {
		klog.Infof("INFO: PV '%s' reclaimPolicy set to %s!", persV.Name, reclaimPolicy)
	}
}


// 0 duration means no reclaiming policy
func getPVReclaimingGracePeriod(persV v1.PersistentVolume) time.Duration {
	reclaimPolicyDuration, err := time.ParseDuration(persV.ObjectMeta.Annotations[annotationPeriodReclaimVolumesAfterRelease])

	if err != nil {
		return 0
	}

	// also consider negative values as invalid
	if reclaimPolicyDuration < 0 {
		return 0
	}

	return reclaimPolicyDuration
}


// for PVs that should be reclaimed after a grace period (as indicated by the annotationPeriodReclaimVolumesAfterRelease annotation),
// calculate the grace period and set it on the PV (via annotation annotationDelete).
// Does nothing if annotationDelete is already present or annotationPeriodReclaimVolumesAfterRelease is not set.
func setPVGracePeriod(persV v1.PersistentVolume) {

	if _, ok := persV.ObjectMeta.Annotations[annotationDelete]; ok {
		klog.Infof("INFO: PersistentVolume %s already has delete annotation", persV.Name)
		return
	}

	reclaimingGracePeriod := getPVReclaimingGracePeriod(persV)

	if reclaimingGracePeriod == 0 {
		// no reclaim policy for this PV, nothing to do
		return
	}

	tFutureDeletionPV := time.Now().Add(reclaimingGracePeriod)

	klog.Infof("INFO: Setting annotation on PV %s so it is deleted after %v", persV.Name, tFutureDeletionPV)
	err := setPVDateAnnotation(persV.Name, annotationDelete, tFutureDeletionPV)
	if err != nil {
		klog.Errorf("ERROR: patching PV %s with annotation %s %v", persV.Name, annotationDelete, tFutureDeletionPV)
		return
	}
}

// If a volume is Released very quickly after it was created, assume we can delete it immediately. It's unlikely to have any useful content.
// This will mitigate issues like OTG0048218, where some provisioning problems can result in PVs created in a loop.
// How much time is meant by "quickly" is configured in the PV annotation annotationNoGracePeriodSinceCreation
func pvCanBeReclaimedImmediately(persV v1.PersistentVolume) bool {

	if getPVReclaimingGracePeriod(persV) == 0 {
		// be conservative: only reclaim volumes that have a valid annotationPeriodReclaimVolumesAfterRelease
		return false
	}

	maximumAgeForImmediateReclaiming, err := time.ParseDuration(persV.ObjectMeta.Annotations[annotationNoGracePeriodSinceCreation])

	if err != nil {
		// be conservative: if we cannot determine a maximum age, then do not delete the PV immediately
		return false
	}

	// also consider negative values as invalid
	if maximumAgeForImmediateReclaiming < 0 {
		return false
	}

	deadLineForImmediateReclaiming := persV.GetCreationTimestamp().Add(maximumAgeForImmediateReclaiming)

	return time.Now().Before(deadLineForImmediateReclaiming)
}


func main() {

	// Initializing global flags for klog
	klog.InitFlags(nil)

	// Called it to parse the command line into the defined flags
	flag.Parse()

	// List *all* persistent volumes
	pvList, err := kubeclient.kubeclient.CoreV1().PersistentVolumes().List(meta_v1.ListOptions{})
	if err != nil {
		klog.Fatalf("ERROR: Impossible to retrieve the list of all persistent volumes - %v", err)
	}

	for _, persV := range pvList.Items {
		// Reclaiming volumes only makes sense for PVs that have been Released
		if persV.Status.Phase == "Released" {
			if pvCanBeReclaimedImmediately(persV) {
				klog.Infof("INFO: deleting PersistentVolume %s immediately as it does have the minimum age to apply grace period", persV.Name)
				requestPVDeletion(persV)
				// nothing else to do for this PV
				continue
			}

			if pvGracePeriodHasExpired(persV) {
				klog.Infof("INFO: deleting PersistentVolume %s now since it is at the end of its grace period", persV.Name)
				requestPVDeletion(persV)
				// nothing else to do for this PV
				continue
			}

			setPVGracePeriod(persV)
		}
	}
	klog.Infof("All existing PersistentVolumes have been processed")
}

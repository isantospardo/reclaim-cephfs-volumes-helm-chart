# NB: expects env var image_to_test to point to the docker image implementing the reclaim-cephfs-volume logic to be tested

# test must fail if there's any error
set -e

function createBoundPV {
    pv_name=$1
    shift
    # the remaining parameters will be passed to `oc annotate` and set annotations on the new PV

    oc create -f - <<EOF
apiVersion: v1
kind: PersistentVolume
metadata:
  name: ${test_name}
spec:
    accessModes:
    - ReadWriteOnce
    capacity: { storage: 1M }
    claimRef:
        apiVersion: v1
        kind: PersistentVolumeClaim
        name: ${test_name}
        namespace: default
    hostPath:
      path: /mnt
EOF

    oc create -f - <<EOF
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: ${test_name}
spec:
    accessModes:
    - ReadWriteOnce
    resources:
        requests:
            storage: 1M
    volumeName: ${test_name}
EOF

    # add all annotations passed in extra parameters to the PV
    if [[ $# > 0 ]]; then
      oc annotate pv/$pv_name $*
    fi

    # wait for PVC binding
    while [ "$(oc get pvc/$test_name -o go-template='{{.status.phase}}')" != "Bound" ]; do
        echo "Waiting for volume binding"
        sleep 1
    done
}

function releasePV {
    pv_name=$1
    oc delete pvc/$pv_name
}

function runReclaimer {
    test_name=$1
    oc run $test_name --rm --attach --image=$image_to_test --restart=Never
}

function checkPVPhase {
    pv_name=$1
    expected_value=$2

    test "$(oc get pv/$pv_name -o go-template='{{.status.phase}}')" == "${expected_value}"
}

function checkPVMarkedForDeletion {
    pv_name=$1
    # maybe the PV has actually been deleted already, so assume 'Delete' if PV is missing
    test "$(oc get pv/$pv_name -o go-template='{{.spec.persistentVolumeReclaimPolicy}}' || echo 'Delete')" == "Delete"
}

function getDeleteAnnotationValue {
    pv_name=$1
    # NB: if annotation does not exist, jq will return "null"

    oc get pv/$pv_name -o json | jq -r '.metadata.annotations."cern.ch/volume-reclaim-deletion-timestamp"'
}


echo "When a PV is not Released"
echo "Then the PV should not be modified"
test_name="no-change-for-bound-pv"
createBoundPV $test_name reclaim-volumes.cern.ch/deletion-grace-period-after-release="720h" reclaim-volumes.cern.ch/no-grace-period-if-time-since-creation-is-less-than="1h"
runReclaimer $test_name
checkPVPhase $test_name "Bound"
test "$(getDeleteAnnotationValue $test_name)" == "null"
echo -e "OK\n"

echo "When a PV is Released"
echo "And it has a delete annotation"
echo "And the date in the delete annotations is in the past"
echo "Then the PV should be marked for deletion"
test_name="pv-with-expired-delete-annotation-mark-for-deletion"
createBoundPV $test_name reclaim-volumes.cern.ch/deletion-grace-period-after-release="720h" reclaim-volumes.cern.ch/no-grace-period-if-time-since-creation-is-less-than="1h" reclaim-volumes.cern.ch/volume-reclaim-deletion-timestamp="2019-01-01T08:19:47Z"
releasePV $test_name
runReclaimer $test_name
checkPVMarkedForDeletion $test_name
echo -e "OK\n"

echo "When a PV is Released"
echo "And it has a delete annotation"
echo "And the date in the delete annotations is not passed yet"
echo "Then the PV should be marked for deletion"
test_name="no-change-for-pv-with-delete-annotation-in-the-future"
next_year="$(( $(date +%Y) + 1 ))-01-01T08:19:47Z"
createBoundPV $test_name reclaim-volumes.cern.ch/deletion-grace-period-after-release="720h" reclaim-volumes.cern.ch/volume-reclaim-deletion-timestamp="${next_year}"
releasePV $test_name
runReclaimer $test_name
checkPVPhase $test_name "Released"
test "$(getDeleteAnnotationValue $test_name)" == "${next_year}"
echo -e "OK\n"

echo "When a PV is Released"
echo "And it has a grace period annotation"
echo "And it has no delete annotation"
echo "Then the PV should get a delete annotation"
test_name="set-delete-annotation-for-released-pv"
createBoundPV $test_name reclaim-volumes.cern.ch/deletion-grace-period-after-release="24h"
releasePV $test_name
runReclaimer $test_name
checkPVPhase $test_name "Released"
test "$(getDeleteAnnotationValue $test_name)" != "null"
echo -e "OK\n"

echo "When a PV is Released"
echo "And it has an invalid grace period annotation"
echo "And it has no delete annotation"
echo "Then the PV should not be modified"
test_name="invalid-grace-period-for-released-pv"
createBoundPV $test_name reclaim-volumes.cern.ch/deletion-grace-period-after-release="dummy"
releasePV $test_name
runReclaimer $test_name
checkPVPhase $test_name "Released"
test "$(getDeleteAnnotationValue $test_name)" == "null"
echo -e "OK\n"

echo "When a PV is Released"
echo "And it has no grace period annotation"
echo "Then the PV should not be modified"
test_name="no-grace-period-for-released-pv"
createBoundPV $test_name
releasePV $test_name
runReclaimer $test_name
checkPVPhase $test_name "Released"
test "$(getDeleteAnnotationValue $test_name)" == "null"
echo -e "OK\n"

echo "When a PV is Released"
echo "And it has a grace period annotation"
echo "And it has a minimum age for grace period to apply"
echo "And it is younger than that age"
echo "Then the PV should be marked for deletion"
test_name="skip-grace-period"
createBoundPV $test_name reclaim-volumes.cern.ch/deletion-grace-period-after-release="24h" reclaim-volumes.cern.ch/no-grace-period-if-time-since-creation-is-less-than="1h"
releasePV $test_name
runReclaimer $test_name
checkPVMarkedForDeletion $test_name
echo -e "OK\n"

echo "When a PV is Released"
echo "And it has a grace period annotation"
echo "And it has a minimum age for grace period to apply"
echo "And it is older than that age"
echo "Then the PV should get a delete annotation"
test_name="dont-skip-grace-period-if-old-enough"
createBoundPV $test_name reclaim-volumes.cern.ch/deletion-grace-period-after-release="24h" reclaim-volumes.cern.ch/no-grace-period-if-time-since-creation-is-less-than="1s"
sleep 2 # longer than no-grace-period-if-time-since-creation-is-less-than annotation
releasePV $test_name
runReclaimer $test_name
checkPVPhase $test_name "Released"
test "$(getDeleteAnnotationValue $test_name)" != "null"
echo -e "OK\n"

echo "When a PV is Released"
echo "And it has no grace period annotation"
echo "And it has a minimum age for grace period to apply"
echo "And it is older than that age"
echo "Then the PV should get a delete annotation"
test_name="no-grace-period-dont-reclaim-even-if-old-enough"
createBoundPV $test_name reclaim-volumes.cern.ch/no-grace-period-if-time-since-creation-is-less-than="1s"
sleep 2 # longer than no-grace-period-if-time-since-creation-is-less-than annotation
releasePV $test_name
runReclaimer $test_name
checkPVPhase $test_name "Released"
test "$(getDeleteAnnotationValue $test_name)" == "null"
echo -e "OK\n"


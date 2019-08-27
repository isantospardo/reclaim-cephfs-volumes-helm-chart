# Reclaim CephFS volumes after a grace period

By default, the `persistentVolumeReclaimPolicy` of a CephFS PV creation is set to `Retain`, this means that even when the user deletes this PV,
it will persist in our infrastructure in order to recover PVs unfortunately removed by mistake.

In order to delete PVs marked for deletion, we have created a cronjob in charge of deleting permanently PVs marked for deletion
in a specific period of time, this `time-reclaim` is set in each of the StorageClasses [CIPAAS-542](https://its.cern.ch/jira/browse/CIPAAS-542)

Once this time-reclaim is reached, the cronJob patches the `spec` of the PV and sets the `persistentVolumeReclaimPolicy` to `Delete`,
this will trigger a permanently deletion of the PV.

In light of INC1973961: to mitigate the impact of something that creates and deletes PVCs in a loop, we immediately delete PVs that were released less than loopCreationTimePV (described in the parametrized values) after being created.

## Parametrized values

In order to interact with this parametrized values, the only requirement is to add the pertinent flag during the execution (e.g. -loopCreationTimePV 5h)

*Possible values:*

- loopCreationTimePV: default to `1h`, PVs released less than the loopCreationTimePV after being created to avoid incidents like INC1973961.
- storageClassName: default to `cephfs`, specifies the storageClassName

## ServiceAccount

- `manila-provisioner`: needed to list and annotate PVs created in the cluster and it is defined in
[CephFS scc deployment](https://gitlab.cern.ch/paas-tools/infrastructure/cephfs-csi-deployment).

## Deployment

This cronjob for CephFS volumes is deployed with `helm` as a subchart of [CephFS csi deployment](https://gitlab.cern.ch/paas-tools/infrastructure/cephfs-csi-deployment).
The namespace used to be deployed is by default `paas-infra-cephfs`, in all the clusters.

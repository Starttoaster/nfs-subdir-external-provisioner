# Kubernetes NFS Subdir External Provisioner

This is a fork of [kubernetes-sigs/nfs-subdir-external-provisioner](https://github.com/kubernetes-sigs/nfs-subdir-external-provisioner). This fork will receive security updates, but it won't receive feature updates. The goal of this project is just to offer a more secure version of the kubernetes-sigs project with a distroless base container image and updated Golang dependencies. Unfortunately, since the upstream project lost maintainership, it had accrued a number of critical CVEs, and runs on an EoL Alpine Linux version.

**NFS subdir external provisioner** is an automatic provisioner that uses your _existing and already configured_ NFS server to support dynamic provisioning of Kubernetes Persistent Volumes via Persistent Volume Claims. Persistent volumes are provisioned as `${namespace}-${pvcName}-${pvName}`.

## How to deploy NFS Subdir External Provisioner to your cluster

To note again, you must _already_ have an NFS Server.

### With Helm

Follow the instructions from the helm chart [README](chart/nfs-subdir-external-provisioner/README.md).

The tl;dr is

```bash
helm repo add nfs-subdir-external-provisioner https://starttoaster.github.io/nfs-subdir-external-provisioner/
helm install nfs-subdir-external-provisioner nfs-subdir-external-provisioner/nfs-subdir-external-provisioner \
    --set nfs.server=x.x.x.x \
    --set nfs.path=/exported/path
```

## NFS provisioner limitations/pitfalls
* The provisioned storage is not guaranteed. You may allocate more than the NFS share's total size. The share may also not have enough storage space left to actually accommodate the request.
* The provisioned storage limit is not enforced. The application can expand to use all the available storage regardless of the provisioned size.
* Storage resize/expansion operations are not presently supported in any form. You will end up in an error state: `Ignoring the PVC: didn't find a plugin capable of expanding the volume; waiting for an external controller to process this PVC.`

# Note on Versioning

Note that this fork continued on with the existing versioning from the upstream kubernetes-sigs project, which abandoned the project at v4.0.2. This means that if kubernetes ever picks up and continues the source project, v4.0.3 and greater in this fork would not be identical code to v4.0.3 and greater in the upstream kubernetes-sigs project. Be aware of this difference if migrating between one or the other. This fork is a compatible replacement for kubernetes-sigs/nfs-subdir-external-provisioner@v4.0.2, which is the latest release there at time of writing.

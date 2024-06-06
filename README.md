# Kubernetes NFS Subdir External Provisioner

This is a fork of an upstream kubernetes-sigs project [here](https://github.com/kubernetes-sigs/nfs-subdir-external-provisioner). The upstream project has seen little support in years, resulting in horribly insecure build artifacts since their images aren't rebuilt over time, and with no new releases they haven't been able to publish a new image with fewer CVEs from the base image. This project is unlikely to see much in the way of features in this fork, but should be more secure to run with a more up-to-date base image and Go dependencies.

**NFS subdir external provisioner** is an automatic provisioner that use your _existing and already configured_ NFS server to support dynamic provisioning of Kubernetes Persistent Volumes via Persistent Volume Claims. Persistent volumes are provisioned as `${namespace}-${pvcName}-${pvName}`.

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

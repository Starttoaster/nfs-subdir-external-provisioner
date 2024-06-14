/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	storagehelpers "k8s.io/component-helpers/storage/volume"
	"k8s.io/klog/v2"

	v1 "k8s.io/api/core/v1"

	storage "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/sig-storage-lib-external-provisioner/v10/controller"
)

const (
	provisionerNameKey = "PROVISIONER_NAME"
)

type nfsProvisioner struct {
	client kubernetes.Interface
	server string
	path   string
}

type pvcMetadata struct {
	data        map[string]string
	labels      map[string]string
	annotations map[string]string
}

var pattern = regexp.MustCompile(`\${\.PVC\.((labels|annotations)\.(.*?)|.*?)}`)

func (meta *pvcMetadata) stringParser(str string) string {
	result := pattern.FindAllStringSubmatch(str, -1)
	for _, r := range result {
		switch r[2] {
		case "labels":
			str = strings.ReplaceAll(str, r[0], meta.labels[r[3]])
		case "annotations":
			str = strings.ReplaceAll(str, r[0], meta.annotations[r[3]])
		default:
			str = strings.ReplaceAll(str, r[0], meta.data[r[1]])
		}
	}

	return str
}

const (
	mountPath      = "/persistentvolumes/"
	archiveSubPath = "_archived_"
)

var _ controller.Provisioner = &nfsProvisioner{}

func (p *nfsProvisioner) Provision(ctx context.Context, options controller.ProvisionOptions) (*v1.PersistentVolume, controller.ProvisioningState, error) {
	logger := klog.FromContext(ctx)

	if options.PVC.Spec.Selector != nil {
		return nil, controller.ProvisioningFinished, fmt.Errorf("claim Selector is not supported")
	}
	logger.Info(fmt.Sprintf("nfs provisioner: VolumeOptions %v", options))

	pvcNamespace := options.PVC.Namespace
	pvcName := options.PVC.Name

	pvName := strings.Join([]string{pvcNamespace, pvcName, options.PVName}, "-")

	metadata := &pvcMetadata{
		data: map[string]string{
			"name":      pvcName,
			"namespace": pvcNamespace,
			"pvname":    options.PVName,
		},
		labels:      options.PVC.Labels,
		annotations: options.PVC.Annotations,
	}

	fullPath := filepath.Join(mountPath, pvName)
	path := filepath.Join(p.path, pvName)

	pathPattern, exists := options.StorageClass.Parameters["pathPattern"]
	if exists {
		customPath := metadata.stringParser(pathPattern)
		if customPath != "" {
			path = filepath.Join(p.path, customPath)
			fullPath = filepath.Join(mountPath, customPath)
		}
	}

	logger.Info(fmt.Sprintf("creating path %s", fullPath))
	if err := os.MkdirAll(fullPath, 0o777); err != nil {
		return nil, controller.ProvisioningFinished, errors.New("unable to create directory to provision new pv: " + err.Error())
	}
	err := os.Chmod(fullPath, 0o777)
	if err != nil {
		return nil, "", err
	}

	pv := &v1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: options.PVName,
		},
		Spec: v1.PersistentVolumeSpec{
			PersistentVolumeReclaimPolicy: *options.StorageClass.ReclaimPolicy,
			AccessModes:                   options.PVC.Spec.AccessModes,
			MountOptions:                  options.StorageClass.MountOptions,
			Capacity: v1.ResourceList{
				v1.ResourceName(v1.ResourceStorage): options.PVC.Spec.Resources.Requests[v1.ResourceName(v1.ResourceStorage)],
			},
			PersistentVolumeSource: v1.PersistentVolumeSource{
				NFS: &v1.NFSVolumeSource{
					Server:   p.server,
					Path:     path,
					ReadOnly: false,
				},
			},
		},
	}
	return pv, controller.ProvisioningFinished, nil
}

func pruneEmptyParents(path string) {
	if filepath.Clean(path) == filepath.Clean(mountPath) {
		return
	}
	err := os.Remove(path)
	if err == nil {
		pruneEmptyParents(filepath.Dir(path))
	}
}

func deleteAndPruneEmptyParents(path string, ctx context.Context) error {
	logger := klog.FromContext(ctx)
	err := os.RemoveAll(path)
	if err != nil {
		return err
	}
	pruneEmptyParents(filepath.Dir(path))
	logger.Info(fmt.Sprintf("path %s and any empty parents have been deleted", path))
	return nil
}

func buildArchivePath(path string) string {
	if filepath.Clean(path) == filepath.Clean(mountPath) {
		return fmt.Sprintf("%s/%s", archiveSubPath, time.Now().Format("200601021504"))
	}
	return fmt.Sprintf("%s.%s", buildArchivePath(filepath.Dir(path)), filepath.Base(path))
}

func (p *nfsProvisioner) Delete(ctx context.Context, volume *v1.PersistentVolume) error {
	logger := klog.FromContext(ctx)

	path := volume.Spec.PersistentVolumeSource.NFS.Path
	oldPath := strings.Replace(path, p.path, mountPath, 1)

	if _, err := os.Stat(oldPath); os.IsNotExist(err) {
		logger.Info(fmt.Sprintf("warning: path %s does not exist, deletion skipped", oldPath))
		return nil
	}
	// Get the storage class for this volume.
	storageClass, err := p.getClassForVolume(ctx, volume)
	if err != nil {
		return err
	}

	// Determine if the "onDelete" parameter exists.
	// If it exists and has a `delete` value, delete the directory.
	// If it exists and has a `retain` value, safe the directory.
	onDelete := storageClass.Parameters["onDelete"]
	switch onDelete {
	case "delete":
		return deleteAndPruneEmptyParents(oldPath, ctx)
	case "retain":
		return nil
	}

	// Determine if the "archiveOnDelete" parameter exists.
	// If it exists and has a false value, delete the directory.
	// Otherwise, archive it.
	archiveOnDelete, exists := storageClass.Parameters["archiveOnDelete"]
	if exists {
		archiveBool, err := strconv.ParseBool(archiveOnDelete)
		if err != nil {
			return err
		}
		if !archiveBool {
			return deleteAndPruneEmptyParents(oldPath, ctx)
		}
	}

	archivePath := filepath.Join(mountPath, buildArchivePath(oldPath))

	if _, err := os.Stat(filepath.Join(mountPath, archiveSubPath)); os.IsNotExist(err) {
		os.MkdirAll(filepath.Join(mountPath, archiveSubPath), 0755)
	}

	err = os.Rename(oldPath, archivePath)
	if err == nil {
		logger.Info(fmt.Sprintf("Archived path %s to %s", oldPath, archivePath))
		pruneEmptyParents(filepath.Dir(oldPath))
	} else {
		logger.Info(fmt.Sprintf("Error archiving path %s to %s", oldPath, archivePath))
	}

	return err
}

// getClassForVolume returns StorageClass.
func (p *nfsProvisioner) getClassForVolume(ctx context.Context, pv *v1.PersistentVolume) (*storage.StorageClass, error) {
	if p.client == nil {
		return nil, fmt.Errorf("cannot get kube client")
	}
	className := storagehelpers.GetPersistentVolumeClass(pv)
	if className == "" {
		return nil, fmt.Errorf("volume has no storage class")
	}
	class, err := p.client.StorageV1().StorageClasses().Get(ctx, className, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	return class, nil
}

func main() {
	flag.Parse()
	_ = flag.Set("logtostderr", "true")

	ctx := context.Background()
	logger := klog.FromContext(ctx)

	server := os.Getenv("NFS_SERVER")
	if server == "" {
		logger.Error(nil, "NFS_SERVER not set")
		os.Exit(1)
	}
	path := os.Getenv("NFS_PATH")
	if path == "" {
		logger.Error(nil, "NFS_PATH not set")
		os.Exit(1)
	}
	provisionerName := os.Getenv(provisionerNameKey)
	if provisionerName == "" {
		logger.Error(nil, fmt.Sprintf("environment variable %s is not set! Please set it.", provisionerNameKey))
		os.Exit(1)
	}

	kubeconfig := os.Getenv("KUBECONFIG")
	var config *rest.Config
	if kubeconfig != "" {
		// Create an OutOfClusterConfig and use it to create a client for the controller
		// to use to communicate with Kubernetes
		var err error
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			logger.Error(err, "failed to create kubeconfig")
			os.Exit(1)
		}
	} else {
		// Create an InClusterConfig and use it to create a client for the controller
		// to use to communicate with Kubernetes
		var err error
		config, err = rest.InClusterConfig()
		if err != nil {
			logger.Error(err, "failed to create in cluster config")
			os.Exit(1)
		}
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		logger.Error(err, "failed to create kubernetes client")
		os.Exit(1)
	}

	// Create the provisioner: it implements the Provisioner interface expected by
	// the controller
	clientNFSProvisioner := &nfsProvisioner{
		client: clientset,
		server: server,
		path:   path,
	}

	// Start the provision controller which will dynamically provision efs NFS
	// PVs
	pc := controller.NewProvisionController(
		logger,
		clientset,
		provisionerName,
		clientNFSProvisioner,
	)

	// Never stops.
	pc.Run(context.Background())
}

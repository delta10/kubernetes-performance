// Copyright © Delta10 B.V. 2020
// Licensed under the EUPLv1.2

package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/svent/go-flags"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/tools/clientcmd"
)

var options struct {
	Namespace    string `long:"namespace" default:"kubernetes-performance" description:"Namespace for the workload"`
	KubeConfig   string `long:"kube-config" env:"KUBECONFIG" default:"" description:"The location of the Kubernetes configuration"`
	Command      string `long:"command" default:"sysbench cpu run --time=30 --threads=4" description:"The command to execute"`
	Cleanup      bool   `long:"cleanup" default:"false" description:"Cleanup pods after run"`
	Pvc          bool   `long:"pvc" default:"" description:"Claim a persistent volume and mount to the pods"`
	StorageClass string `long:"storage-class" default:"standard" description:"Persistent volume storage class"`
}

func main() {
	var fsGroup = int64(1000)

	args, err := flags.Parse(&options)
	if err != nil {
		if et, ok := err.(*flags.Error); ok {
			if et.Type == flags.ErrHelp {
				return
			}
		}
		log.Fatalf("error parsing flags: %v", err)
	}

	if len(args) > 0 {
		log.Fatalf("unexpected arguments: %v", args)
	}

	if options.KubeConfig == "" {
		options.KubeConfig = filepath.Join(homeDir(), ".kube", "config")
	}

	config, err := clientcmd.BuildConfigFromFlags("", options.KubeConfig)
	if err != nil {
		panic(err.Error())
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	nodes, err := clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		panic(err.Error())
	}

	fmt.Printf("Nodes:\n")

	for _, node := range nodes.Items {
		fmt.Printf("%s\n", node.Name)
	}

	fmt.Printf("\n")

	pods, err := clientset.CoreV1().Pods(options.Namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		panic(err.Error())
	}

	fmt.Printf("There are %d pods in the namespace\n", len(pods.Items))

	for _, node := range nodes.Items {
		if options.Pvc {
			pvc := &apiv1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("kubernetes-performance-%s", node.Name),
				},
				Spec: apiv1.PersistentVolumeClaimSpec{
					StorageClassName: &options.StorageClass,
					AccessModes:      []apiv1.PersistentVolumeAccessMode{apiv1.ReadWriteOnce},
					Resources: apiv1.ResourceRequirements{
						Requests: apiv1.ResourceList{
							"storage": resource.MustParse("1Gi"),
						},
					},
				},
			}

			_, err := clientset.CoreV1().PersistentVolumeClaims(options.Namespace).Create(context.TODO(), pvc, metav1.CreateOptions{})
			if err != nil {
				panic(err.Error())
			}
		}

		pod := &apiv1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("kubernetes-performance-%s", node.Name),
			},
			Spec: apiv1.PodSpec{
				NodeName: node.Name,
				SecurityContext: &apiv1.PodSecurityContext{
					FSGroup: &fsGroup,
				},
				Containers: []apiv1.Container{
					{
						Name:    "kubernetes-performance",
						Image:   "registry.gitlab.com/delta10/kubernetes-performance:latest",
						Command: strings.Split(options.Command, " "),
					},
				},
				RestartPolicy: apiv1.RestartPolicyNever,
			},
		}

		if options.Pvc {
			pod.Spec.Containers[0].VolumeMounts = []apiv1.VolumeMount{
				{
					Name:      "pvc",
					MountPath: "/pvc",
				},
			}

			pod.Spec.Volumes = []apiv1.Volume{
				{
					Name: "pvc",
					VolumeSource: apiv1.VolumeSource{
						PersistentVolumeClaim: &apiv1.PersistentVolumeClaimVolumeSource{
							ClaimName: fmt.Sprintf("kubernetes-performance-%s", node.Name),
						},
					},
				},
			}
		}

		_, err := clientset.CoreV1().Pods(options.Namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
		if err != nil {
			panic(err.Error())
		}
	}

	var podsCompleted bool

	fmt.Printf("Waiting for pods to complete...\n")

	for {
		pods, err = clientset.CoreV1().Pods(options.Namespace).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			panic(err.Error())
		}

		podsCompleted = true
		for _, pod := range pods.Items {
			if pod.Status.Phase == apiv1.PodPending || pod.Status.Phase == apiv1.PodRunning {
				podsCompleted = false
			}
		}

		if podsCompleted == false {
			fmt.Printf("Waiting for pods to complete...\n")
			time.Sleep(5 * time.Second)
		} else {
			break
		}
	}

	pods, err = clientset.CoreV1().Pods(options.Namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		panic(err.Error())
	}

	for _, pod := range pods.Items {
		req := clientset.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, &apiv1.PodLogOptions{})
		podLogs, err := req.Stream(context.TODO())
		if err != nil {
			panic(err.Error())
		}
		defer podLogs.Close()

		logFile, err := os.Create(fmt.Sprintf("%s.log", pod.Name))
		if err != nil {
			panic(err.Error())
		}

		defer logFile.Close()

		_, err = io.Copy(logFile, podLogs)
		if err != nil {
			panic(err.Error())
		}

		if options.Cleanup {
			err = clientset.CoreV1().Pods(options.Namespace).Delete(context.TODO(), pod.Name, metav1.DeleteOptions{})
			if err != nil {
				panic(err.Error())
			}
		}
	}

	if options.Cleanup {
		pvcs, err := clientset.CoreV1().PersistentVolumeClaims(options.Namespace).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			panic(err.Error())
		}

		for _, pvc := range pvcs.Items {
			err = clientset.CoreV1().PersistentVolumeClaims(options.Namespace).Delete(context.TODO(), pvc.Name, metav1.DeleteOptions{})
			if err != nil {
				panic(err.Error())
			}
		}
	}
}

func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	return os.Getenv("USERPROFILE") // windows
}

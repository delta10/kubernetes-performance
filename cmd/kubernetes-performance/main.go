// Copyright © Delta10 B.V. 2020
// Licensed under the EUPLv1.2

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
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
	Nodes        string `long:"nodes" default:"" description:"Restrict to a comma-separated list of nodes"`
	Namespace    string `long:"namespace" default:"kubernetes-performance" description:"Namespace for the workload"`
	KubeConfig   string `long:"kube-config" env:"KUBECONFIG" default:"" description:"The location of the Kubernetes configuration"`
	Cleanup      bool   `long:"cleanup" default:"false" description:"Cleanup pods after run"`
	Pvc          bool   `long:"pvc" default:"false" description:"Claim a persistent volume and mount to the pods"`
	EmptyDir     bool   `long:"empty-dir" default:"false" description:"Claim an empty dir and mount to the pods"`
	StorageClass string `long:"storage-class" default:"standard" description:"Persistent volume storage class"`

	Command string `long:"command" default:"" description:"Run a specific benchmark command"`

	SaturateCluster        bool `long:"saturate-cluster" default:"false" description:"Saturate the cluster"`
	ReplicationControllers int  `long:"replication-controllers" default:"1" description:"Number of replication controllers for saturation test"`
	Replicas               int  `long:"replicas" default:"30" description:"Number of replicas for saturation test"`

	Network     bool   `long:"network" default:"false" description:"Load test network"`
	NetworkTime string `long:"network-time" default:"60" description:"Time of network load test"`
}

var fsGroup = int64(1000)

func main() {
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

	var selectedNodes []string
	for _, node := range nodes.Items {
		if options.Nodes == "" {
			selectedNodes = append(selectedNodes, node.Name)
		} else {
			for _, selectedNode := range strings.Split(options.Nodes, ",") {
				if node.Name == selectedNode {
					selectedNodes = append(selectedNodes, node.Name)
				}
			}
		}
	}

	fmt.Printf("Selected %d nodes\n", len(selectedNodes))

	if options.Command != "" {
		runCommandDistributed(clientset, selectedNodes)
	}

	if options.SaturateCluster {
		saturateCluster(clientset)
	}

	if options.Network {
		saturateNetwork(clientset, selectedNodes)
	}
}

func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	return os.Getenv("USERPROFILE") // windows
}

func runCommandDistributed(clientset *kubernetes.Clientset, nodes []string) {
	for _, node := range nodes {
		if options.Pvc {
			pvc := &apiv1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("kubernetes-performance-%s", node),
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
				Name: fmt.Sprintf("kubernetes-performance-%s", node),
			},
			Spec: apiv1.PodSpec{
				NodeName: node,
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
							ClaimName: fmt.Sprintf("kubernetes-performance-%s", node),
						},
					},
				},
			}
		}

		if options.EmptyDir {
			pod.Spec.Containers[0].VolumeMounts = []apiv1.VolumeMount{
				{
					Name:      "emptydir",
					MountPath: "/emptydir",
				},
			}

			pod.Spec.Volumes = []apiv1.Volume{
				{
					Name: "emptydir",
					VolumeSource: apiv1.VolumeSource{
						EmptyDir: &apiv1.EmptyDirVolumeSource{},
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
		pods, err := clientset.CoreV1().Pods(options.Namespace).List(context.TODO(), metav1.ListOptions{})
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

	pods, err := clientset.CoreV1().Pods(options.Namespace).List(context.TODO(), metav1.ListOptions{})
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

func saturateCluster(clientset *kubernetes.Clientset) {
	fmt.Printf("Saturating cluster...")

	replicas := int32(options.Replicas)
	userGuid := int64(1000)

	replicationController := &apiv1.ReplicationController{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("kubernetes-performance-saturate"),
		},
		Spec: apiv1.ReplicationControllerSpec{
			Replicas: &replicas,
			Selector: map[string]string{
				"app": "kubernetes-performance-saturate",
			},
			Template: &apiv1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name: "kubernetes-performance-saturate",
					Labels: map[string]string{
						"app": "kubernetes-performance-saturate",
					},
				},
				Spec: apiv1.PodSpec{
					Containers: []apiv1.Container{
						{
							Name:  fmt.Sprintf("kubernetes-performance-saturate"),
							Image: "k8s.gcr.io/pause:3.1",
						},
					},
					SecurityContext: &apiv1.PodSecurityContext{
						RunAsUser: &userGuid,
					},
				},
			},
		},
	}

	_, err := clientset.CoreV1().ReplicationControllers(options.Namespace).Create(context.TODO(), replicationController, metav1.CreateOptions{})
	if err != nil {
		panic(err.Error())
	}

	for {
		replicationController, err = clientset.CoreV1().ReplicationControllers(options.Namespace).Get(context.TODO(), "kubernetes-performance-saturate", metav1.GetOptions{})
		if replicationController.Status.AvailableReplicas != replicas {
			fmt.Printf("Waiting for replicas to be running...\n")
			time.Sleep(5 * time.Second)
		} else {
			break
		}
	}

	pods, err := clientset.CoreV1().Pods(options.Namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		panic(err.Error())
	}

	readyTimes := make([]float64, len(pods.Items))

	for i, pod := range pods.Items {
		for _, condition := range pod.Status.Conditions {
			if condition.Type == apiv1.PodReady {
				readyTimes[i] = condition.LastTransitionTime.Time.Sub(pod.CreationTimestamp.Time).Seconds()
			}
		}
	}

	data, _ := json.Marshal(readyTimes)

	_ = ioutil.WriteFile("pod-startup-times.json", data, 0644)

	if options.Cleanup {
		zero := int32(0)
		replicationController.Spec.Replicas = &zero
		_, err := clientset.CoreV1().ReplicationControllers(options.Namespace).Update(context.TODO(), replicationController, metav1.UpdateOptions{})
		if err != nil {
			panic(err.Error())
		}

		for {
			replicationController, err = clientset.CoreV1().ReplicationControllers(options.Namespace).Get(context.TODO(), "kubernetes-performance-saturate", metav1.GetOptions{})
			if replicationController.Status.AvailableReplicas != zero {
				fmt.Printf("Waiting for replication controller scaled to zero...\n")
				time.Sleep(5 * time.Second)
			} else {
				break
			}
		}

		err = clientset.CoreV1().ReplicationControllers(options.Namespace).Delete(context.TODO(), "kubernetes-performance-saturate", metav1.DeleteOptions{})
		if err != nil {
			panic(err.Error())
		}
	}
}

func saturateNetwork(clientset *kubernetes.Clientset, nodes []string) {
	if len(nodes) < 2 {
		fmt.Printf("Cannot perform network load test. Require a minimum of two nodes.")
		return
	}

	serverPod := &apiv1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("kubernetes-performance-network-server-%s", nodes[0]),
		},
		Spec: apiv1.PodSpec{
			NodeName: nodes[0],
			Containers: []apiv1.Container{
				{
					Name:    "kubernetes-performance",
					Image:   "registry.gitlab.com/delta10/kubernetes-performance:latest",
					Command: []string{"iperf3", "-s"},
				},
			},
			RestartPolicy: apiv1.RestartPolicyNever,
		},
	}

	_, err := clientset.CoreV1().Pods(options.Namespace).Create(context.TODO(), serverPod, metav1.CreateOptions{})
	if err != nil {
		panic(err.Error())
	}

	fmt.Printf("Waiting for server to start...\n")

	for {
		serverPod, err = clientset.CoreV1().Pods(options.Namespace).Get(context.TODO(), serverPod.Name, metav1.GetOptions{})
		if err != nil {
			panic(err.Error())
		}

		if serverPod.Status.PodIP == "" {
			fmt.Printf("Waiting for server to start...\n")
			time.Sleep(5 * time.Second)
		} else {
			break
		}
	}

	clientPod := &apiv1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("kubernetes-performance-network-client-%s", nodes[1]),
		},
		Spec: apiv1.PodSpec{
			NodeName: nodes[1],
			Containers: []apiv1.Container{
				{
					Name:    "kubernetes-performance",
					Image:   "registry.gitlab.com/delta10/kubernetes-performance:latest",
					Command: []string{"iperf3", "-c", serverPod.Status.PodIP, "-t", options.NetworkTime},
				},
			},
			RestartPolicy: apiv1.RestartPolicyNever,
		},
	}

	_, err = clientset.CoreV1().Pods(options.Namespace).Create(context.TODO(), clientPod, metav1.CreateOptions{})
	if err != nil {
		panic(err.Error())
	}

	fmt.Printf("Running performance test\n")

	for {
		clientPod, err = clientset.CoreV1().Pods(options.Namespace).Get(context.TODO(), clientPod.Name, metav1.GetOptions{})
		if err != nil {
			panic(err.Error())
		}

		if clientPod.Status.Phase == apiv1.PodPending || clientPod.Status.Phase == apiv1.PodRunning {
			fmt.Printf("Waiting for test to finish...\n")
			time.Sleep(5 * time.Second)
		} else {
			break
		}
	}

	for _, pod := range []string{clientPod.Name, serverPod.Name} {
		req := clientset.CoreV1().Pods(options.Namespace).GetLogs(pod, &apiv1.PodLogOptions{})
		podLogs, err := req.Stream(context.TODO())
		if err != nil {
			panic(err.Error())
		}
		defer podLogs.Close()

		logFile, err := os.Create(fmt.Sprintf("%s.log", pod))
		if err != nil {
			panic(err.Error())
		}

		defer logFile.Close()

		_, err = io.Copy(logFile, podLogs)
		if err != nil {
			panic(err.Error())
		}

		if options.Cleanup {
			err = clientset.CoreV1().Pods(options.Namespace).Delete(context.TODO(), pod, metav1.DeleteOptions{})
			if err != nil {
				panic(err.Error())
			}
		}
	}
}

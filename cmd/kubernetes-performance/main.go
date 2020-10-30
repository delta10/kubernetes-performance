// Copyright © Delta10 B.V. 2020
// Licensed under the EUPLv1.2

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	cli "github.com/jawher/mow.cli"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/tools/clientcmd"
)

var options struct {
	Nodes      string
	Namespace  string
	KubeConfig string
}

var (
	fsGroup       = int64(1000)
	clientset     *kubernetes.Clientset
	selectedNodes []string
)

func main() {
	fmt.Printf("Kubernetes Performance - Copyright © Delta10 B.V. 2020 - Licensed under the EUPL v1.2.\n\n")

	app := cli.App("kubernetes-performance", "Run performance tests on a Kubernetes cluster")

	app.StringPtr(&options.Nodes, cli.StringOpt{
		Name:   "nodes",
		Desc:   "Comma-seperated list of nodes to use (default: all nodes)",
		EnvVar: "NODES",
	})

	app.StringPtr(&options.Namespace, cli.StringOpt{
		Name:   "namespace",
		Desc:   "Namespace the benchmark will be run in",
		EnvVar: "NAMESPACE",
		Value:  "kubernetes-performance",
	})

	app.StringPtr(&options.KubeConfig, cli.StringOpt{
		Name:   "kubeconfig",
		Desc:   "Path to Kubernetes configuration file",
		EnvVar: "KUBECONFIG",
		Value:  filepath.Join(homeDir(), ".kube", "config"),
	})

	app.Command("saturate", "Saturate the cluster with pods to benchmark control plane", cmdSaturate)
	app.Command("run", "Run benchmark command (sysbench or fio) on all nodes", cmdRun)
	app.Command("network", "Run iperf3 on first two nodes to benchmark network connection", cmdNetwork)

	app.Before = func() {
		config, err := clientcmd.BuildConfigFromFlags("", options.KubeConfig)
		if err != nil {
			panic(err.Error())
		}

		clientset, err = kubernetes.NewForConfig(config)
		if err != nil {
			panic(err.Error())
		}

		nodes, err := clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			panic(err.Error())
		}

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
	}

	app.Run(os.Args)
}

func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	return os.Getenv("USERPROFILE") // windows
}

func cmdSaturate(cmd *cli.Cmd) {
	var (
		replicas = cmd.IntOpt("replicas", 30, "Number of replicas for saturation test")
	)

	cmd.Action = func() {
		fmt.Printf("Saturating cluster...\n")

		determinedReplicas := int32(*replicas)
		userGUID := int64(1000)

		replicationController := &apiv1.ReplicationController{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("kubernetes-performance-saturate"),
			},
			Spec: apiv1.ReplicationControllerSpec{
				Replicas: &determinedReplicas,
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
							RunAsUser: &userGUID,
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
			if replicationController.Status.AvailableReplicas != determinedReplicas {
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

		fmt.Printf("Scaling down to zero...\n")

		zero := int32(0)
		replicationController.Spec.Replicas = &zero
		_, err = clientset.CoreV1().ReplicationControllers(options.Namespace).Update(context.TODO(), replicationController, metav1.UpdateOptions{})
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

func cmdRun(cmd *cli.Cmd) {
	var (
		claimPvc       = cmd.BoolOpt("claim-pvc", false, "Claim a persistent volume and mount on /pvc")
		createEmptyDir = cmd.BoolOpt("create-empty-dir", false, "Create an empty dir on /emptydir")
		storageClass   = cmd.StringOpt("storage-class", "standard", "The name of the storage class")
		command        = cmd.StringArg("COMMAND", "", "The command that is run")
	)

	cmd.Spec = "[OPTIONS] [COMMAND] [OPTIONS]"

	cmd.Action = func() {
		for _, node := range selectedNodes {
			if *claimPvc {
				pvc := &apiv1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf("kubernetes-performance-%s", node),
					},
					Spec: apiv1.PersistentVolumeClaimSpec{
						StorageClassName: storageClass,
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
							Command: strings.Split(*command, " "),
						},
					},
					RestartPolicy: apiv1.RestartPolicyNever,
				},
			}

			if *claimPvc {
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

			if *createEmptyDir {
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

			err = clientset.CoreV1().Pods(options.Namespace).Delete(context.TODO(), pod.Name, metav1.DeleteOptions{})
			if err != nil {
				panic(err.Error())
			}
		}

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

func cmdNetwork(cmd *cli.Cmd) {
	var (
		testTime = cmd.IntOpt("time", 30, "Time to run the test in seconds")
	)

	cmd.Action = func() {
		if len(selectedNodes) < 2 {
			fmt.Printf("Cannot perform network load test. Require a minimum of two nodes.")
			return
		}

		serverPod := &apiv1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("kubernetes-performance-network-server-%s", selectedNodes[0]),
			},
			Spec: apiv1.PodSpec{
				NodeName: selectedNodes[0],
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
				Name: fmt.Sprintf("kubernetes-performance-network-client-%s", selectedNodes[1]),
			},
			Spec: apiv1.PodSpec{
				NodeName: selectedNodes[1],
				Containers: []apiv1.Container{
					{
						Name:    "kubernetes-performance",
						Image:   "registry.gitlab.com/delta10/kubernetes-performance:latest",
						Command: []string{"iperf3", "-c", serverPod.Status.PodIP, "-t", fmt.Sprintf("%d", *testTime)},
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

			err = clientset.CoreV1().Pods(options.Namespace).Delete(context.TODO(), pod, metav1.DeleteOptions{})
			if err != nil {
				panic(err.Error())
			}
		}
	}
}

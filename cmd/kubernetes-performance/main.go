// Copyright © Delta10 B.V. 2020
// Licensed under the EUPLv1.2

package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/svent/go-flags"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

var options struct {
	Namespace  string `long:"namespace" default:"kubernetes-performance" description:"Namespace for the workload"`
	KubeConfig string `long:"kube-config" env:"KUBECONFIG" default:"" description:"The location of the Kubernetes configuration"`
	Command    string `long:"command" default:"" description:"The command to execute"`
}

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
		pod := &apiv1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("kubernetes-performance-%s", node.Name),
			},
			Spec: apiv1.PodSpec{
				NodeName: node.Name,
				Containers: []apiv1.Container{
					{
						Name:    "kubernetes-performance",
						Image:   "nginx:1.12",
						Command: []string{"echo", "1"},
					},
				},
				RestartPolicy: apiv1.RestartPolicyNever,
			},
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
			if pod.Status.Phase == apiv1.PodPending {
				podsCompleted = false
			}
		}

		if podsCompleted != true {
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

		buf := new(bytes.Buffer)
		_, err = io.Copy(buf, podLogs)
		if err != nil {
			panic(err.Error())
		}

		fmt.Printf("Logs %s: %s\n", pod.Name, buf.String())

		err = clientset.CoreV1().Pods(options.Namespace).Delete(context.TODO(), pod.Name, metav1.DeleteOptions{})
		if err != nil {
			panic(err.Error())
		}
	}
}

func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	return os.Getenv("USERPROFILE") // windows
}

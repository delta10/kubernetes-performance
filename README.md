# Kubernetes performance

This repository contains tools to perform benchmarks on Kubernetes clusters.

- Control plane: measure API-responsiveness and pod startup time.
- Workers: measure performance of CPU, memory, network, local disks and persistent volumes.

## Control plane

```bash
go run cmd/clusterloader.go --provider=local --kubeconfig=/Users/bart/.kube/config --testconfig=testing/density/config.yaml --report-dir=reports/
```

```bash
go run cmd/clusterloader.go --provider=local --kubeconfig=/Users/bart/.kube/config --testconfig=testing/load/config.yaml --report-dir=reports/
```

## Workers

```bash
docker build -t benchmark -f benchmark/Dockerfile .
```

### CPU

```bash
sysbench cpu run --time=30 --threads=4
```

### Memory

```bash
sysbench memory run --memory-block-size=1M --memory-total-size=4G --threads=4
```

### Disk

```bash
fio --name=randread --rw=randread --direct=1 --ioengine=libaio --bs=8k --numjobs=16 --size=1G --runtime=30 --group_reporting --filename=/tmp/test
fio --name=randwrite --rw=randwrite --direct=1 --ioengine=libaio --bs=64k --numjobs=8 --size=512m --runtime=30 --group_reporting --filename=/tmp/test
```

### Network

Server

```bash
kubectl run benchmark-network-server --image=benchmark --image-pull-policy IfNotPresent -- iperf3 -s
```

Client

```bash
kubectl run -it --rm benchmark-network-client --image=benchmark --image-pull-policy IfNotPresent -- iperf3 -c $(kubectl get pod benchmark-network-server --template={{.status.podIP}})
```

```bash
kubectl delete pod benchmark-network-server
```

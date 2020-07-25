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
kubectl run -it --rm benchmark-cpu --image=benchmark --image-pull-policy IfNotPresent -- sysbench cpu run --time=10 --threads=1
```

### Memory

```bash
kubectl run -it --rm benchmark-memory --image=benchmark --image-pull-policy IfNotPresent -- sysbench memory run --memory-block-size=1M --memory-total-size=4G --threads=1
```

### Disk

```bash
kubectl run -it --rm benchmark-disk --image=benchmark --image-pull-policy IfNotPresent -- fio --name=randwrite --iodepth=1 --rw=randwrite --bs=4m --size=256M --filename=/tmp/test
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

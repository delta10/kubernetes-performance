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
fio --name=randrw --rw=randrw --direct=1 --ioengine=libaio --bs=4k --iodepth=256 --numjobs=4 --size=1G --runtime=30 --group_reporting --filename=/tmp/test
fio --name=randread --rw=randread --direct=1 --ioengine=libaio --bs=8k --numjobs=16 --size=1G --runtime=30 --group_reporting --filename=/tmp/test
fio --name=randwrite --rw=randwrite --direct=1 --ioengine=libaio --bs=64k --numjobs=8 --size=512m --runtime=30 --group_reporting --filename=/tmp/test
fio --name=randrw --rw=randrw --direct=1 --ioengine=libaio --bs=4k --iodepth=256 --numjobs=4 --size=1G --runtime=30 --group_reporting --filename=/tmp/test
```

### Network

Server

```bash
iperf3 -s
```

Client

```bash
iperf3 -c {pod-ip}
```

## Contributing

Contributions are welcome, see [CONTRIBUTING.md](CONTRIBUTING.md) for more details. By contributing to this project, you accept and agree the the terms and conditions as specified in the [Contributor Licence Agreement](CLA.md).

## Licence

The software is distributed under the EUPLv1.2 licence, see the [LICENCE](LICENCE) file.

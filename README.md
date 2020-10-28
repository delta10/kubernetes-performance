# Kubernetes performance

This repository contains tools to perform benchmarks on Kubernetes clusters.

- Control plane: measure API-responsiveness and pod startup time.
- Workers: measure performance of CPU, memory, network, local disks and persistent volumes.

## Control plane

To measure the control plane performance run:

```bash
kubernetes-performance saturate --replicas 10
```

## Workers

```bash
docker build -t benchmark -f benchmark/Dockerfile .
```

### CPU

```bash
kubernetes-performance run "sysbench cpu run --time=30 --threads=4"
```

### Memory

```bash
kubernetes-performance run "sysbench memory run --memory-block-size=1M --memory-total-size=4G --threads=4"
```

### Disk

```bash
kubernetes-performance run "fio --name=randrw --rw=randrw --direct=1 --ioengine=libaio --bs=4k --iodepth=256 --numjobs=4 --size=1G --runtime=30 --group_reporting --filename=/tmp/test"
```

### Network

```bash
kubernetes-performance network
```

## Contributing

Contributions are welcome, see [CONTRIBUTING.md](CONTRIBUTING.md) for more details. By contributing to this project, you accept and agree the the terms and conditions as specified in the [Contributor Licence Agreement](CLA.md).

## Licence

The software is distributed under the EUPLv1.2 licence, see the [LICENCE](LICENCE) file.

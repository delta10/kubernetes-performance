# Kubernetes performance

This tool can be used to measure a Kubernetes cluster for performance. It provides features to measure the following metrics:

- Control plane: measure API-responsiveness and pod startup time.
- Workers: measure performance of CPU, memory, network, local disks and persistent volumes.

## Install

Download the latest release from the [releases page](https://gitlab.com/delta10/kubernetes-performance/-/releases). Then unpack it:

```bash
$ unzip kubernetes-performance-v1.0-darwin-amd64.zip
```

Find the unpacked binary and move it to its desired destination:

```bash
$ mv kubernetes-performance-darwin-amd64 /usr/local/bin/kubernetes-performance
```

Now you should be able to run kubernetes-performance.

## Measure control plane

The control plane performance is measured by saturating the cluster with pods and measuring the pod startup time. The number of pods is specified with `--replicas`. This can be used to determine if a cluster adheres to the [SLA as specified by the Kubernetes project](https://kubernetes.io/blog/2015/09/kubernetes-performance-measurements-and/):

1. "API-responsiveness": 99% of all our API calls return in less than 1 second
2. "Pod startup time": 99% of pods (with pre-pulled images) start within 5 seconds

Run the tests with:

```bash
$ kubernetes-performance saturate --replicas 10
```

The pod startup times are reported in pod-startup-times.json. To determine the API-responsiveness make sure Prometheus is pre-installed on the cluster. Use the following Prometheus query to determine the responsiveness, grouped by request type:

```
histogram_quantile(0.99, sum(rate(apiserver_request_duration_seconds_bucket{verb!="WATCH", subresource!="proxy"}[1m]))  by (verb, scope, le))
```

## Measure workers

The performance of the workers is measured by scheduling a pod per worker and run [sysbench](https://github.com/akopytov/sysbench), [fio](https://fio.readthedocs.io/) and [iperf3](https://iperf.fr/) on the worker. Then the logs are collected and reported locally in *.log.

### CPU

Run a CPU benchmark with the following command:

```bash
$ kubernetes-performance run "sysbench cpu run --time=10 --threads=4"
```

### Memory

Run a memory benchmark with the following command:

```bash
$ kubernetes-performance run "sysbench memory run --memory-block-size=1M --memory-total-size=4G --threads=4"
```

### Disk

The performance of disks can be measured both for local storage and persistent volumes. To test the performance of an [emptyDir](https://kubernetes.io/docs/concepts/storage/volumes/#emptydir) use:

```bash
$ kubernetes-performance run "fio --name=randrw --rw=randrw --direct=1 --ioengine=libaio --bs=4k --iodepth=256 --numjobs=4 --size=1G --runtime=30 --group_reporting --filename=/emptydir/test" --create-empty-dir
```

To benchmark a persistent volume, the tool provides the ability to claim a [persistent volume](https://kubernetes.io/docs/concepts/storage/persistent-volumes/#reserving-a-persistentvolume) of a specific storage class. For example to test the performance of a persistent volume from the storage class faster use:

```bash
$ kubernetes-performance run "fio --name=randrw --rw=randrw --direct=1 --ioengine=libaio --bs=4k --iodepth=256 --numjobs=4 --size=512Mi --runtime=30 --group_reporting --filename=/pvc/test" --claim-pvc --storage-class=faster
```

### Network

To benchmark the performance of a cluster a minimum of two nodes are required. The tool wil schedule a iperf3 server on the first node and a iperf3 client on the second node. Run a benchmark with:

```bash
$ kubernetes-performance network
```

Reports of both the server and client are reported in *.log.

## Contributing

Contributions are welcome, see [CONTRIBUTING.md](CONTRIBUTING.md) for more details. By contributing to this project, you accept and agree the the terms and conditions as specified in the [Contributor Licence Agreement](CLA.md).

## Licence

The software is distributed under the EUPLv1.2 licence, see the [LICENCE](LICENCE) file.

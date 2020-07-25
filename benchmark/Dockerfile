FROM debian:buster

RUN apt-get update && \
    apt-get install -y curl

RUN curl -s https://packagecloud.io/install/repositories/akopytov/sysbench/script.deb.sh | bash

RUN apt-get install -y sysbench fio iperf3

RUN rm -rf /var/lib/apt/lists/*

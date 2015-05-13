# Docker Image

## Self-contained Kubernetes-Mesos with Docker-Compose

The provided docker-compose.yml contains a self-contained configuration for running kubernetes-mesos, including its dependencies (etcd & mesos).
It will launch 5 docker containers linked together with hostnames and port forwarding.

```
# from inside the docker dir
docker-compose up
```

[Docker Compose](https://docs.docker.com/compose/) can be installed via apt-get, homebrew, or [manually](https://docs.docker.com/compose/install/).

## Build Docker image

```
# from root of project, not inside the docker dir
docker build -f ./docker/Dockerfile -t mesosphere/kubernetes-mesos .
```

## Run Docker container

The Dockerfile includes everything needed to run a development instance of kubernetes-mesos, including etcd and mesos.

### Background mode

```
docker run --name kubernetes-mesos -p 8888:8888 -p 5050:5050 -p 4001:4001 mesosphere/kubernetes-mesos &> /tmp/kubernetes-mesos-docker.log &
```

To attach in interactive mode to a container already running in background mode, use:

```
docker exec -it kubernetes-mesos /bin/bash
```

## Interactive mode

```
docker run --name kubernetes-mesos -p 8888:8888 -p 5050:5050 -p 4001:4001  -i -t --entrypoint=/bin/bash mesosphere/kubernetes-mesos
```

Note: Interactive mode launches bash instead of the start script.

## Stopping

```
docker kill kubernetes-mesos
```

## Starting kubernetes-mesos locally or in docker interactive mode (with etcd & mesos)

```
$ ./scripts/start.sh
```

Example output:

```
Kubernetes: 1.2.3.4:8888
Mesos: 1.2.3.4:5050
Etcd: http://localhost:4001
Config: /Users/<you>/go/src/github.com/mesosphere/kubernetes-mesos/mesos-cloud.conf
Writing default config
Log Dir: /tmp/k8sm-logs
---------------------
Starting etcd
Waiting (up to 10s) for etcd to accept connections
Connection to localhost port 4001 [tcp/newoak] succeeded!
---------------------
Starting mesos-local
Waiting (up to 10s) for mesos-local to accept connections
Connection to 1.2.3.4 port 5050 [tcp/mmcc] succeeded!
---------------------
Starting km apiserver
Waiting (up to 10s) for km apiserver to accept connections
Connection to 1.2.3.4 port 8888 [tcp/ddi-tcp-1] succeeded!
---------------------
Starting km controller-manager
---------------------
Starting km scheduler
---------------------
```
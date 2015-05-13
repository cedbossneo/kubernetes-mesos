#!/usr/bin/env bash

# Mac support requires gtimeout & pcregrep (brew install coreutils && brew install pcre)

cmd_exists () {
    local CMD_NAME="$1"
    return $(type -p ${CMD_NAME} &> /dev/null)
}

pid_running () {
    local CMD_PID="$1"
    return $(kill -0 ${CMD_PID} &> /dev/null)
}

TIMEOUT=$(which timeout)
if ! cmd_exists "timeout"; then
    if cmd_exists "gtimeout"; then
        # Mac support
        TIMEOUT=$(which gtimeout)
    else
        echo "timeout or gtimeout command not found"
        exit 1
    fi
fi

ETCD=$(which etcd)
if ! cmd_exists "etcd"; then
    echo "etcd command not found"
    exit 1
fi

MESOS=$(which mesos-local)
if ! cmd_exists "mesos-local"; then
    echo "mesos-local command not found"
    exit 1
fi

K8SM="$(pwd)/bin/km"
if [ ! -e ${K8SM} ]; then
    echo "./bin/km command not found"
    exit 1
fi

set -e

if cmd_exists "ifconfig" && cmd_exists "ipconfig" && cmd_exists "pcregrep"; then
    PUBLIC_IP=$(ifconfig | pcregrep -M -o '^[^\t:]+:([^\n]|\n\t)*status: active' | egrep -o -m 1 '^[^\t:]+' | xargs ipconfig getifaddr)
fi

if [ -z "${PUBLIC_IP}" ] && cmd_exists "ip"; then
    PUBLIC_IP=$(ip addr | grep 'state UP' -A2 | tail -n1 | awk '{print $2}' | cut -f1  -d'/')
fi

if [ -z "${PUBLIC_IP}" ]; then
    echo "Public IP Not Found. Using 0.0.0.0"
    PUBLIC_IP="0.0.0.0"
fi

export K8SM_HOST=${PUBLIC_IP}
export K8SM_PORT=8888
echo "Kubernetes: ${K8SM_HOST}:${K8SM_PORT}"

export MESOS_HOST=${PUBLIC_IP}
export MESOS_PORT=5050
export MESOS_MASTER=${MESOS_HOST}:${MESOS_PORT}
echo "Mesos: ${MESOS_MASTER}"

export ETCD_HOST=${PUBLIC_IP}
export ETCD_PORT=4001
export ETCD_URL=http://${ETCD_HOST}:${ETCD_PORT}
echo "Etcd: ${ETCD_URL}"

K8SM_CONFIG="$(pwd)/mesos-cloud.conf"
echo "Config: ${K8SM_CONFIG}"
if [ ! -f "$K8SM_CONFIG" ]; then
    echo "Writing default config"
    echo "[mesos-cloud]
        http-client-timeout = 5s
        state-cache-ttl     = 20s
    " > $K8SM_CONFIG
fi

LOG_DIR="/tmp/k8sm-logs"
mkdir -p ${LOG_DIR}
echo "Log Dir: ${LOG_DIR}"

echo "---------------------"

set -e

SUBPROCS=()
SUBPROC_NAMES=()

await_connection () {
    export SERVER_HOST="$1"
    export SERVER_PORT="$2"
    local SERVER_NAME="$3"
    echo "Waiting (up to 10s) for ${SERVER_NAME} to accept connections"
    if ! ${TIMEOUT} 10 bash -c "while ! echo exit | nc -z ${SERVER_HOST} ${SERVER_PORT}; do sleep 0.5; done"; then
        echo "Timed out"
        exit 1
    fi
}

# Add the pid of a sub-process or exit if it is not running
add_subproc () {
    local SUBPROC="$1"
    local SUBPROC_NAME="$2"
    if ! ps aux | grep -v grep | grep "${SUBPROC_NAME}" &> /dev/null; then
        echo "Failed to start ${SUBPROC_NAME}"
        exit 1
    fi
#    echo "Running ${SUBPROC_NAME} (${SUBPROC})"
    SUBPROCS+=(${SUBPROC})
    SUBPROC_NAME=${SUBPROC_NAME// /_} # replace spaces with underscores (bash arrays are space deliniated)
    SUBPROC_NAMES+=(${SUBPROC_NAME})
}

# Kill subprocesses in reverse order of being started (to avoid log spam)
kill_subprocs () {
    echo "Shutting down"
    set +e
    for ((i=${#SUBPROCS[@]}-1; i>=0; i--)); do
        local SUBPROC=${SUBPROCS[i]}
        local SUBPROC_NAME=${SUBPROC_NAMES[i]}
        local SUBPROC_NAME=${SUBPROC_NAME//_/ } # replace underscores with spaces
        if pid_running "${SUBPROC}"; then
            echo "Killing ${SUBPROC_NAME} (${SUBPROC})"
            kill ${SUBPROC}
        fi
    done
    set -e
}

trap 'kill_subprocs' EXIT

echo "Starting etcd"
${ETCD} \
  &> ${LOG_DIR}/etcd.log \
  &
add_subproc "$!" "etcd"
await_connection "${ETCD_HOST}" "${ETCD_PORT}" "etcd"
echo "---------------------"


echo "Starting mesos-local"
${MESOS} \
  &> ${LOG_DIR}/mesos.log \
  &
add_subproc "$!" "mesos-local"
await_connection "${MESOS_HOST}" "${MESOS_PORT}" "mesos-local"
echo "---------------------"


echo "Starting km apiserver"
${K8SM} apiserver \
  --address=${K8SM_HOST} \
  --etcd_servers=${ETCD_URL} \
  --portal_net=10.10.10.0/24 \
  --port=${K8SM_PORT} \
  --cloud_provider=mesos \
  --cloud_config=${K8SM_CONFIG} \
  &> ${LOG_DIR}/apiserver.log \
  &
add_subproc "$!" "km apiserver"
await_connection "${K8SM_HOST}" "${K8SM_PORT}" "km apiserver"
echo "---------------------"


echo "Starting km controller-manager"
${K8SM} controller-manager \
  --master=${K8SM_HOST}:${K8SM_PORT} \
  --cloud_config=${K8SM_CONFIG} \
  &> ${LOG_DIR}/controller-manager.log \
  &
add_subproc "$!" "km controller-manager"
echo "---------------------"


echo "Starting km scheduler"
${K8SM} scheduler \
  --mesos_master=${MESOS_MASTER} \
  --address=${K8SM_HOST} \
  --etcd_servers=${ETCD_URL} \
  --mesos_user=root \
  --api_servers=${K8SM_HOST}:${K8SM_PORT} \
  &> ${LOG_DIR}/scheduler.log \
  &
add_subproc "$!" "km scheduler"
echo "---------------------"

for subproc in "${SUBPROCS[@]}"; do
    if pid_running "${subproc}"; then
        wait ${subproc}
    fi
done
#!/bin/bash

set -e

#================================================
# Variables
#================================================

# Cluster name and size.
CLUSTER_NAME="cls"
CLUSTER_SIZE=3

# Image to use for cluster instances.
INSTANCE_IMAGE="ubuntu:22.04"

# Type of cluster instances (container or virtual-machine).
INSTANCE_TYPE="container"

# Version of LXD to install.
VERSION_LXD="latest/edge"

# Other.
INSTANCE="${CLUSTER_NAME}"
LEADER="${CLUSTER_NAME}-1"
STORAGE_POOL="${CLUSTER_NAME}-pool"
STORAGE_DRIVER="dir"
NETWORK_NAME="${CLUSTER_NAME}br0"

#================================================
# Utils
#================================================

# waitInstance waits for the VM to become ready.
waitInstance() {
        local instance="$1"
        local timeout="${2:-60}"

        if [ "${instance}" = "" ]; then
                echo "Error: waitInstance: missing argument: instance name"
                return 1
        fi

        echo "Waiting instance ${instance} to become ready ..."
        for j in $(seq 1 "${timeout}"); do
                local procCount=$(lxc info "${instance}" | awk '/Processes:/ {print $2}')
                if [ "${procCount:-0}" -gt 0 ]; then
                        echo "Instance ${instance} ready after ${j} seconds."
                        break
                fi

                if [ "${j}" -ge "${timeout}" ]; then
                        echo "Error: Instance ${instance} still not ready after ${timeout} seconds!"
                        return 1
                fi

                sleep 1
        done
}

# instanceIPv4 returns the IPv4 address of the instance with the given name.
instanceIPv4() {
        instance="$1"

        # Try for enp5s0 (VM) and eth0 (container) interfaces.
        for inf in enp5s0 eth0; do
                ipv4=$(lxc ls "${instance}" -f csv -c 4 | grep -oP "(\d{1,3}\.){3}\d{1,3}(?= \(${inf}\))" || true)
                if [ "${ipv4}" != "" ]; then
                        echo "${ipv4}"
                        return
                fi
        done

        echo "Error: Failed to obtain IPv4 address of instance ${instance}"
        return 1
}


#========================
# Cluster setup
#========================

# deploy deploys instances required for a LXD cluster.
deploy() {
        # Create dedicated network.
        echo "Creating network ${NETWORK_NAME} ..."
        exists=$(lxc network list --format csv | awk -F, '{print $1}' | grep "${NETWORK_NAME}" || true)
        if [ ! "${exists}" ]; then
                lxc network create "${NETWORK_NAME}"
        fi

        # Create storage pool.
        echo "Creating storage pool ${STORAGE_POOL} ..."
        exists=$(lxc storage list --format csv | awk -F, '{print $1}' | grep "${STORAGE_POOL}" || true)
        if [ ! "${exists}" ]; then
                lxc storage create "${STORAGE_POOL}" zfs
        fi

        # Setup cluster VMs.
        for i in $(seq 1 "${CLUSTER_SIZE}"); do
                instance="${INSTANCE}-${i}"

                echo "Creating instance ${instance} ..."
                exists=$(lxc list --format csv --columns n "${instance}")
                if [ "${exists}" != "" ]; then
                        echo "Instance ${instance} is already running"
                        continue
                fi

                args=""
                if [ "${INSTANCE_TYPE}" = "virtual-machine" ]; then
                        args="--vm"
                else
                        args="-c security.nesting=true"
                fi

                lxc launch "${INSTANCE_IMAGE}" "${instance}" \
                        --storage "${STORAGE_POOL}" \
                        --network "${NETWORK_NAME}" \
                        -c limits.cpu=4 \
                        -c limits.memory=4GiB \
                        $args
        done

        # Wait for instances to become ready.
        for i in $(seq 1 "${CLUSTER_SIZE}"); do
                instance="${INSTANCE}-${i}"
                waitInstance "${instance}"
                lxc exec "${instance}" -- systemctl is-system-running --wait
        done

        # Install LXD on VMs.
        for i in $(seq 1 "${CLUSTER_SIZE}"); do
                instance="${INSTANCE}-${i}"

                echo "Preparing instance ${instance} ..."

                # Install snap daemon.
                lxc exec "${instance}" --env=DEBIAN_FRONTEND=noninteractive -- bash -c "\
                        apt-get -qq -y install snapd" > /dev/null

                # Install LXD snap.
                lxc exec "${instance}" -- bash -c "snap install lxd --channel ${VERSION_LXD} || sudo snap refresh lxd --channel ${VERSION_LXD}" > /dev/null
        done

        echo "Cluster instances created."
        lxc list
}

# configure_lxd configures LXD cluster.
configure_lxd() {
        echo "Creating LXD cluster ..."

        # Create LXD cluster.
        for i in $(seq 1 "${CLUSTER_SIZE}"); do
                instance="${INSTANCE}-${i}"

                isClustered=$(lxc exec "${instance}" -- bash -c "lxc cluster list 2> /dev/null" || true)
                if [ "${isClustered}" ]; then
                        continue
                fi

                # Get IPv4 of the instance.
                ipv4=$(instanceIPv4 "${instance}")

                # On the leader instance, just enable clustering and continue.
                if [ "${instance}" = "${LEADER}" ]; then
                        lxc exec "${instance}" -- lxc config set core.https_address "${ipv4}"
                        lxc exec "${instance}" -- lxc cluster enable "${instance}"
                        continue
                fi

                # Create and extract token for a new cluster member.
                token=$(lxc exec "${LEADER}" -- lxc cluster add -q "${instance}")
                if [ "${token}" = "" ]; then
                        echo "Error: Failed retrieveing join token for instance ${instance}"
                        exit 1
                fi

                # Create temporary preseed config file.
                config="/tmp/${instance}.cluster.cfg"
                echo -e "\
cluster:
  enabled: true
  server_address: ${ipv4}
  cluster_token: ${token}
" > "${config}"

                # Apply the config and remove the temporary file.
                cat "${config}" | lxc exec "${instance}" -- bash -c "lxd init --preseed"
                rm "${config}"
        done

        # Create default storage pool.
        exists=$(lxc exec "${LEADER}" -- bash -c "lxc storage list" | grep "default" || true)
        if [ ! "${exists}" ]; then
                for i in $(seq 1 "${CLUSTER_SIZE}"); do
                        instance="${INSTANCE}-${i}"
                        lxc exec "${LEADER}" -- bash -c "lxc storage create default ${STORAGE_DRIVER} --target ${instance}"
                done

                lxc exec "${LEADER}" -- bash -c "lxc storage create default ${STORAGE_DRIVER}"
                lxc exec "${LEADER}" -- bash -c "lxc profile device add default root disk pool=default path=/"

                # Resize default storage.
                if [ "${STORAGE_DRIVER}" != "dir" ]; then
                        for i in $(seq 1 "${CLUSTER_SIZE}"); do
                                instance="${INSTANCE}-${i}"
                                lxc exec "${LEADER}" -- bash -c "lxc storage set default size 3GiB --target ${instance}"
                        done
                fi
        fi

        # Create default managed network (lxdbr0).
        exists=$(lxc exec "${LEADER}" -- lxc network list | grep "lxdbr0" || true)
        if [ ! "${exists}" ]; then
                for i in $(seq 1 "${CLUSTER_SIZE}"); do
                        instance="${INSTANCE}-${i}"
                        lxc exec "${LEADER}" -- lxc network create lxdbr0 --target "${instance}"
                done

                lxc exec "${LEADER}" -- lxc network create lxdbr0
                lxc exec "${LEADER}" -- lxc profile device add default eth0 nic nictype=bridged parent=lxdbr0
        fi

        # Configure new cluster remote.
        token=$(lxc exec "${LEADER}" -- lxc config trust add --name host --quiet)
        ipv4=$(instanceIPv4 "${LEADER}")

        lxc remote rm "${CLUSTER_NAME}" 2>/dev/null || true
        lxc remote add "${CLUSTER_NAME}" "${ipv4}" --token "${token}"
        lxc remote switch "${CLUSTER_NAME}"

        # Show final cluster.
        lxc cluster list "${CLUSTER_NAME}:"
}


#================================================
# Cleanup
#================================================

# cleanup removes the deployed resources.
#
cleanup() {
        # Remove VMs.
        echo "Removing instances ..."
        for i in $(seq 1 "${CLUSTER_SIZE}"); do
                instance="${INSTANCE}-${i}"
                lxc delete "${instance}" --force || true
        done

        # Remove storage pool.
        echo "Removing storage pool ${STORAGE_POOL} ..."
        lxc storage delete "${STORAGE_POOL}" || true

        # Remove network.
        echo "Removing network ${NETWORK_NAME} ..."
        lxc network delete "${NETWORK_NAME}"  || true

        # Remove remote.
        lxc remote switch local
        lxc remote rm "${CLUSTER_NAME}" 2>/dev/null || true
}

#================================================
# Script
#================================================

case "$1" in
        deploy)
                echo "==> RUN: Deploy"

                deploy
                configure_lxd

                echo ""
                echo "==> DONE: LXD cluster created"
                ;;
        cleanup)
                echo "==> RUN: Cleanup"
                cleanup

                echo ""
                echo "==> Done: LXD cluster removed"
                ;;
        *)
                echo "Unkown action: $1"
                echo "Valid actions are: [deploy, cleanup]"
                echo "Run: $0 <action>"
                exit 1
                ;;
esac

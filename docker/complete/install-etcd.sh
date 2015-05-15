#!/usr/bin/env bash

set -ex

ETCD_ARCHIVE_URL=https://github.com/coreos/etcd/releases/download/v0.4.6/etcd-v0.4.6-linux-amd64.tar.gz
ETCD_ARCHIVE=/tmp/$(basename $ETCD_ARCHIVE_URL)

echo "Downloading etcd..."
wget -q $ETCD_ARCHIVE_URL -O $ETCD_ARCHIVE
mkdir -p /tmp/etcd
tar xf $ETCD_ARCHIVE -C /tmp/etcd
mv /tmp/etcd/*/etcd* /usr/local/bin/

rm -f $ETCD_ARCHIVE
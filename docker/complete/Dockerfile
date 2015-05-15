FROM mesosphere/mesos:0.22.0-1.0.ubuntu1404
MAINTAINER Mesosphere <support@mesosphere.io>

RUN locale-gen en_US.UTF-8
RUN dpkg-reconfigure locales
ENV LANG en_US.UTF-8
ENV LC_ALL en_US.UTF-8

RUN apt-get update && \
    apt-get upgrade -y && \
    apt-get install --no-install-recommends -y \
        g++ \
        make \
        curl \
        mercurial \
        git \
        rsync \
        patch \
        wget \
        && \
    apt-get clean

COPY . /kubernetes-mesos
WORKDIR /kubernetes-mesos

ENV GOROOT /usr/local/go
ENV PATH $GOROOT/bin:$PATH

RUN ./docker/complete/install-go.sh

RUN ./docker/complete/install-etcd.sh

RUN make && make install DESTDIR=/kubernetes-mesos/bin

ENV PATH /kubernetes-mesos/bin:/kubernetes-mesos/bin/docker/bin:$PATH

ADD ./docker/mesos-cloud.conf /opt/

EXPOSE 8888 5050 4001
ENTRYPOINT ["km-complete"]
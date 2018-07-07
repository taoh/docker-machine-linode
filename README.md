# docker-machine-linode

Linode Driver Plugin for docker-machine. **Requires docker-machine version > v.0.5.0-rc1**

# Install

First, docker-machine v0.5.0 rc2 is required, documentation for how to install `docker-machine`
[is available here](https://github.com/docker/machine/releases/tag/v0.5.0-rc2#Installation).

or you can install `docker-machine` from source code by running these commands

```bash
go get github.com/docker/machine
cd $GOPATH/src/github.com/docker/machine
make build
```

Then, install `docker-machine-linode` driver in the $GOPATH and add $GOPATH/bin to the $PATH env. 

```bash
go get github.com/displague/docker-machine-linode
cd $GOPATH/src/github.com/displague/docker-machine-linode
make
make install
```

# Run

```bash
docker-machine create -d linode --linode-token=<linode-token> --linode-root-pass=<linode-root-pass> linode
```

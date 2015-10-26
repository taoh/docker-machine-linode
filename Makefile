default: build

clean:
	$(RM) ./bin/docker-machine-driver-linode
	$(RM) $(GOPATH)/bin/docker-machine-driver-linode

build: clean
	GOGC=off go build -i -o ./bin/docker-machine-driver-linode ./bin

install: build
	cp ./bin/docker-machine-driver-linode $(GOPATH)/bin/
	
.PHONY: build install
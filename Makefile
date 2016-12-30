# meta information.

GIT_VER := $(shell git describe --tags)

.PHONY: test packages clean

all: diffrelo

## Setup
setup:
	go get github.com/pkg/sftp
	go get github.com/udhos/equalfile

diffrelo: diffrelo.go
	go build -ldflags "-X main.version ${GIT_VER}"

test:
	go test

packages:
	gox -os="linux darwin" -arch="amd64" -output "pkg/{{.Dir}}-${GIT_VER}-{{.OS}}-{{.Arch}}" -ldflags "-X main.version ${GIT_VER}"
	cd pkg && find . -name "*${GIT_VER}*" -type f -exec zip {}.zip {} \;

clean:
	rm -f pkg/* diffrelo

version = "0.0.1"

build:
	@go build -ldflags "-X=main.version=${version} -X=main.commit=`git rev-parse HEAD`" -o petra

install:
	@make build
	@mv petra ${GOPATH}/bin
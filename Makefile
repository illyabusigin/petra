version = "0.0.1"

cli:
	@cd cmd && go build -ldflags "-X=main.version=${version} -X=main.commit=`git rev-parse HEAD`" -o petra

install-cli:
	@make cli
	@mv cmd/petra ${GOPATH}/bin
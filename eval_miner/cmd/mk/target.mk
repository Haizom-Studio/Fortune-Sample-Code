clean:
	rm -Rf bin
	echo "" > $(APPNAME).log
	go clean

mod:
	go mod tidy

build:
	go build -ldflags=$(LDFLAGS)
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o bin/$(APPNAME)-arm64-linux -ldflags=$(LDFLAGS)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o bin/$(APPNAME)-amd64-linux -ldflags=$(LDFLAGS)

debug:
	go build -ldflags=$(LDFLAGS) -tags debug

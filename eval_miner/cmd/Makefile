include mk/def.mk

all: lint compile
	CI=true go test -ldflags=$(LDFLAGS) ../...

skiput: compile

compile:
	make -C miner

debug:
	make -C miner debug

lint:
	-cd .. ; golangci-lint run -v --config=../.golangci.yml

test:
	CI=true go test -ldflags=$(LDFLAGS) ../...

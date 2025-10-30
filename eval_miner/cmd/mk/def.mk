BRANCH := $(shell git rev-parse --abbrev-ref HEAD)
CGO_ENABLED=0
MODEL=GC22
IMG_DATE=$(shell sh -c "date +"%Y-%m.%d"")
# VERSION=$(BRANCH)-$(USER)
VERSION=$(IMG_DATE)
GITHASH=$(shell sh -c "git rev-parse HEAD")
BUILDTS=$(shell sh -c "date -u")
GITBRANCH := $(shell git rev-parse --abbrev-ref HEAD)
CONTAINER_ID=$(shell sh -c "docker ps | grep $(IMG_NAME)" | awk '{print $$1}')

LDFLAGS="-X 'gcminer/version.Version=$(VERSION)' -X 'gcminer/version.GitHash=$(GITHASH)' -X 'gcminer/version.BuildTS=$(BUILDTS)' -X 'gcminer/version.Branch=$(BRANCH)' -X 'gcminer/version.User=$(USER)' "
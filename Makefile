default: all

VERSION = 1.1.0

BUILD_DIR = $(PWD)/build

GO = go
GO_SOURCE_FILES = $(shell find . -type f -name "*.go")
GO_PACKAGES = $(shell $(GO) list . ./cmd/...)

BINARIES = $(BUILD_DIR)/notifier

$(BUILD_DIR)/notifier: $(GO_SOURCE_FILES)
	$(GO) build -i -o $(BUILD_DIR)/notifier github.com/atombender/gcloud-operations-slack-notifier/cmd

all: binaries

clean:
	rm -f $(BUILD_DIR)/notifier

build:
	$(GO) build -i $(GO_PACKAGES)

binaries: $(BINARIES)

docker-push:
	docker build -t atombender/gcloud-operations-slack-notifier:v$(VERSION) .
	docker tag atombender/gcloud-operations-slack-notifier:v$(VERSION) atombender/gcloud-operations-slack-notifier:latest
	docker push atombender/gcloud-operations-slack-notifier:v$(VERSION)
	docker push atombender/gcloud-operations-slack-notifier:latest

.PHONY: default all clean build binaries docker-push

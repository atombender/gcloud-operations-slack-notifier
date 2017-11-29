default: all

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

.PHONY: default all clean build binaries

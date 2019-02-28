ARG GO_VERSION=1.12

FROM golang:${GO_VERSION}-alpine AS build
RUN apk update && apk add git
RUN mkdir -p /go/src/github.com/atombender/gcloud-operations-slack-notifier
WORKDIR /go/src/github.com/atombender/gcloud-operations-slack-notifier/
ENV GO111MODULE=on
COPY go.mod go.sum ./
RUN go mod download
COPY . ./
RUN CGO_ENABLED=0 go build -o /notifier ./cmd

FROM scratch
COPY --from=build /notifier /notifier
ENTRYPOINT ["/notifier"]

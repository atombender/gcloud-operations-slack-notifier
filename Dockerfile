ARG GO_VERSION=1.12

FROM golang:${GO_VERSION}-alpine AS build
RUN apk add --no-cache curl git ca-certificates \
  && addgroup -S build && adduser -S -G build build
ENV GO111MODULE=on
RUN mkdir -p /mnt/build && chown build:build /mnt/build
WORKDIR /mnt/build
COPY --chown=build:build go.mod go.sum ./
USER build
RUN go mod download
COPY --chown=build:build . ./
RUN CGO_ENABLED=0 go build -o /mnt/build/notifier ./cmd
USER root

FROM build AS base
RUN echo 'nobody:x:65534:65534:nobody:/:' > /etc/passwd \
  && echo 'nobody:x:65534:' > /etc/group

FROM scratch
COPY --from=base /etc/passwd /etc/group /etc/
COPY --from=base /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /mnt/build/notifier /notifier
ENTRYPOINT ["/notifier"]

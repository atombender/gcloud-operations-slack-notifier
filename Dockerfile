FROM golang:1.9-alpine AS build
RUN \
     apk update \
  && apk add git \
  && go get -u github.com/golang/dep/cmd/dep
RUN mkdir -p /go/src/github.com/atombender/gcloud-operations-slack-notifier
WORKDIR /go/src/github.com/atombender/gcloud-operations-slack-notifier/
COPY . .
RUN \
     dep ensure \
  && go build -o /notifier github.com/atombender/gcloud-operations-slack-notifier/cmd

FROM golang:1.9-alpine
COPY --from=build /notifier /srv/
ENTRYPOINT ['/srv/notifier']

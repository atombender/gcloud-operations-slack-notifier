# Slack notifier for Google Cloud Platform operations

A simple notifier that reports new Google Cloud Platform operations to Slack.

## Requirements

* Go >= 1.11

## Building

```shell
$ make
```

## Running

```shell
$ ./notifier \
  --project=myproject \
  --slack-url=https://hooks.slack.com/services/...
  --channel=notification
```

An internal database `state.db` will be written to the current directory. It is used to maintain the state about what's been sent to Slack, so that on the next setup, duplicate events are not reported.

# License

MIT. See `LICENSE`.
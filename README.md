# Slack notifier for Google Cloud Platform operations

A simple notifier that reports new Google Cloud Platform operations to Slack.

## Requirements

* Go >= 1.7

## Building

```shell
$ make
```

## Running

```shell
$ ./notifier \
  --project=myproject \
  --slack-url=https://hooks.slack.com/services/...
```

# License

MIT. See `LICENSE`.
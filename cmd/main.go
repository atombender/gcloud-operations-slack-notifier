package main

import (
	"log"
	"time"

	"github.com/spf13/pflag"

	notifier "github.com/atombender/gcloud-operations-slack-notifier"
)

func main() {
	options := notifier.ReporterOptions{}

	var intervalSecs = 30

	pflag.StringSliceVarP(&options.ProjectIDs, "project", "p", nil,
		"Project ID (required; multiple can be provided).")
	pflag.StringVar(&options.SlackURL, "slack-url", "",
		"Slack webhook URL (required).")
	pflag.StringVarP(&options.Zone, "zone", "z", "",
		"Zone (defaults to all zones).")
	pflag.StringVar(&options.ChannelName, "channel", "",
		"Channel (overrides Slack configuration).")
	pflag.IntVarP(&intervalSecs, "interval", "i", intervalSecs,
		"Polling interval, in seconds (> 0).")
	pflag.Parse()

	options.Interval = time.Second * time.Duration(intervalSecs)

	reporter, err := notifier.NewReporter(options)
	if err != nil {
		log.Fatal(err)
	}
	defer reporter.Shutdown()
	reporter.Run()
}

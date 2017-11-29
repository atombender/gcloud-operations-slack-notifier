package main

import (
	"log"
	"time"

	"github.com/ogier/pflag"

	"github.com/atombender/gcloud-operations-slack-notifier"
)

func main() {
	options := notifier.ReporterOptions{}
	var intervalSecs = 30

	pflag.StringVarP(&options.ProjectID, "project", "p", "",
		"Project ID (required).")
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
	reporter.Run()
}

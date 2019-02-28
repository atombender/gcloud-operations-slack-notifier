package notifier

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path"
	"time"

	"github.com/boltdb/bolt"
	humanize "github.com/dustin/go-humanize"
	"github.com/jpillora/backoff"
	"github.com/lytics/slackhook"
	"github.com/pkg/errors"
	"golang.org/x/net/context"
	"golang.org/x/oauth2/google"
	container "google.golang.org/api/container/v1"
)

const ageThresholdForDisplayingTime = 3 * time.Minute

type ReporterOptions struct {
	ProjectIDs  []string
	ChannelName string
	Zone        string
	StatePath   string
	Interval    time.Duration
	SlackURL    string
}

type Reporter struct {
	options     ReporterOptions
	slackClient *slackhook.Client
	db          *bolt.DB
	syncState   string
}

func NewReporter(options ReporterOptions) (*Reporter, error) {
	if options.Interval <= 0 {
		return nil, errors.New("Interval must be set")
	}

	if options.SlackURL == "" {
		return nil, errors.New("Slack URL must be specified")
	}

	if len(options.ProjectIDs) == 0 {
		return nil, errors.New("At least one project ID must be specified")
	}

	if options.StatePath == "" {
		wd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		options.StatePath = wd
	}

	fileName := path.Join(options.StatePath, "state.db")
	log.Printf("Using state database %s", fileName)

	db, err := bolt.Open(fileName, 0600, nil)
	if err != nil {
		return nil, errors.Wrapf(err, "Could not open BoltDB database %s", fileName)
	}
	defer func() {
		if db != nil {
			db.Close()
		}
	}()

	state := ""
	if err := db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketName)
		if b == nil {
			var err error
			b, err = tx.CreateBucket(bucketName)
			if err != nil {
				return err
			}
		}
		state = string(b.Get(keySyncState))
		return nil
	}); err != nil {
		return nil, err
	}
	if state == "" {
		state = syncStateInitial
	}

	reporter := Reporter{
		options:     options,
		db:          db,
		slackClient: slackhook.New(options.SlackURL),
		syncState:   state,
	}
	db = nil
	return &reporter, nil
}

func (reporter *Reporter) Shutdown() {
	if reporter.db != nil {
		reporter.db.Close()
		reporter.db = nil
	}
}

func (reporter *Reporter) Run() {
	ctx := context.Background()

	if err := reporter.doIteration(ctx); err != nil {
		log.Fatal(err)
	}
	if err := reporter.setSyncState(syncStateIncremental); err != nil {
		log.Fatal(err)
	}

	timer := time.Tick(reporter.options.Interval)
	for {
		select {
		case <-timer:
			if err := reporter.doIteration(ctx); err != nil {
				log.Fatal(err)
			}
		}
	}
}

func (reporter *Reporter) doIteration(ctx context.Context) error {
	boff := &backoff.Backoff{
		Min:    100 * time.Millisecond,
		Max:    10 * time.Second,
		Factor: 2,
		Jitter: false,
	}
	for {
		err := reporter.poll(ctx)
		if err == nil {
			return nil
		}
		if _, ok := err.(SlackNotificationError); !ok {
			return err
		}
		log.Printf("Warning: Unable to notify Slack, will retry: %s", err)
		time.Sleep(boff.Duration())
	}
}

func (reporter *Reporter) poll(ctx context.Context) error {
	c, err := google.DefaultClient(ctx, container.CloudPlatformScope)
	if err != nil {
		log.Fatal(err)
	}

	containerService, err := container.New(c)
	if err != nil {
		log.Fatal(err)
	}

	var zone = reporter.options.Zone
	if zone == "" {
		zone = "-"
	}

	for _, projectID := range reporter.options.ProjectIDs {
		log.Printf("Polling for operations for project %s", projectID)
		resp, err := containerService.Projects.Zones.Operations.List(
			projectID, zone).Context(ctx).Do()
		if err != nil {
			return errors.Wrap(err, "Unable to list operations")
		}
		for _, op := range resp.Operations {
			if err := reporter.addOperation(projectID, op); err != nil {
				return errors.Wrap(err, "Unable to record an operation")
			}
		}
	}
	return nil
}

func (reporter *Reporter) addOperation(
	projectID string,
	op *container.Operation) error {
	entry, err := reporter.getEntry(projectID, op)
	if err != nil {
		return err
	}
	if entry != nil && entry.Status == op.Status {
		log.Printf("Ignoring already-reported operation %s", op.Name)
		return nil
	}

	shouldReport := reporter.syncState == "incremental"
	if shouldReport {
		if err := reporter.reportOperation(projectID, op); err != nil {
			return err
		}
	} else {
		log.Printf("Not reporting operation as we're not incremental yet: %s", op.Name)
	}

	return reporter.saveEntry(projectID, op)
}

func (reporter *Reporter) reportOperation(
	projectID string,
	op *container.Operation) error {
	log.Printf("Notifying Slash with operation %#v", op)

	title := fmt.Sprintf("Cluster operation `%s`", op.OperationType)
	if op.Status != "" {
		title += fmt.Sprintf(" is `%s`", op.Status)
		if op.StatusMessage != "" {
			title += fmt.Sprintf(": %s", op.StatusMessage)
		}
	}

	message := slackhook.Message{
		Channel: reporter.options.ChannelName,
		Text:    title,
	}

	message.AddAttachment(&slackhook.Attachment{
		Title: "Project",
		Text:  fmt.Sprintf("`%s`", projectID),
	})
	if reporter.options.Zone != "" && op.Zone != reporter.options.Zone {
		message.AddAttachment(&slackhook.Attachment{
			Title: "Zone",
			Text:  fmt.Sprintf("`%s`", op.Zone),
		})
	}

	if startTime := reporter.getStartTime(op); startTime != nil {
		message.AddAttachment(&slackhook.Attachment{
			Title: "Since",
			Text:  humanize.Time(*startTime),
		})
	}

	if err := reporter.slackClient.Send(&message); err != nil {
		return SlackNotificationError{Err: err}
	}
	return nil
}

func (reporter *Reporter) getStartTime(op *container.Operation) *time.Time {
	startTime, err := time.Parse(time.RFC3339Nano, op.StartTime)
	if err != nil {
		return nil
	}

	includeTime := time.Since(startTime) > ageThresholdForDisplayingTime
	if !includeTime && op.EndTime != "" {
		if t, err := time.Parse(time.RFC3339Nano, op.EndTime); err == nil {
			includeTime = time.Since(t) > ageThresholdForDisplayingTime
		}
	}
	if includeTime {
		return &startTime
	}
	return nil
}

func (reporter *Reporter) saveEntry(projectID string, op *container.Operation) error {
	return reporter.db.Batch(func(tx *bolt.Tx) error {
		entry := Entry{
			Name:      op.Name,
			Zone:      op.Zone,
			ProjectID: projectID,
			Timestamp: time.Now(),
			Status:    op.Status,
		}
		b, err := json.Marshal(entry)
		if err != nil {
			return err
		}
		return tx.Bucket(bucketName).Put(reporter.keyForOperation(projectID, op), b)
	})
}

func (reporter *Reporter) keyForOperation(
	projectID string,
	op *container.Operation) []byte {
	return []byte(fmt.Sprintf("%s--%s--%s", projectID, op.Zone, op.Name))
}

func (reporter *Reporter) getEntry(
	projectID string,
	op *container.Operation) (*Entry, error) {
	var entry *Entry
	err := reporter.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketName).Get(reporter.keyForOperation(projectID, op))
		if b == nil {
			return nil
		}
		var e Entry
		if err := json.Unmarshal(b, &e); err != nil {
			return err
		}
		entry = &e
		return nil
	})
	return entry, err
}

func (reporter *Reporter) setSyncState(state string) error {
	if state == reporter.syncState {
		return nil
	}

	log.Printf("Transitioning to sync state: %s", state)

	if err := reporter.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketName).Put(keySyncState, []byte(state))
	}); err != nil {
		return err
	}
	reporter.syncState = state
	return nil
}

type Entry struct {
	Name      string    `json:"name"`
	Zone      string    `json:"zone"`
	ProjectID string    `json:"projectID"`
	Timestamp time.Time `json:"timestamp"`
	Status    string    `json:"status"`
}

var bucketName = []byte("default")
var keySyncState = []byte("syncState")

const (
	syncStateInitial     = "initial"
	syncStateIncremental = "incremental"
)

type SlackNotificationError struct {
	Err error
}

func (err SlackNotificationError) Error() string {
	return err.Err.Error()
}

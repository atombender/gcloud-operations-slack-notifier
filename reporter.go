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

type ReporterOptions struct {
	ProjectID string
	Zone      string
	StatePath string
	Interval  time.Duration
	SlackURL  string
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

	if options.ProjectID == "" {
		return nil, errors.New("Project ID must be specified")
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

func (reporter *Reporter) Close() {
	if reporter.db != nil {
		reporter.db.Close()
	}
}

func (reporter *Reporter) Run() {
	if err := reporter.DoIteration(); err != nil {
		log.Fatal(err)
	}
	if err := reporter.setSyncState(syncStateIncremental); err != nil {
		log.Fatal(err)
	}

	timer := time.Tick(reporter.options.Interval)
	for {
		select {
		case <-timer:
			if err := reporter.DoIteration(); err != nil {
				log.Fatal(err)
			}
		}
	}
}

func (reporter *Reporter) DoIteration() error {
	boff := &backoff.Backoff{
		Min:    100 * time.Millisecond,
		Max:    10 * time.Second,
		Factor: 2,
		Jitter: false,
	}

	for {
		err := reporter.Poll()
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

func (reporter *Reporter) Poll() error {
	ctx := context.Background()

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

	log.Printf("Polling for operations")
	resp, err := containerService.Projects.Zones.Operations.List(
		reporter.options.ProjectID, zone).Context(ctx).Do()
	if err != nil {
		return errors.Wrap(err, "Unable to list operations")
	}
	for _, op := range resp.Operations {
		if err := reporter.addOperation(op); err != nil {
			return errors.Wrap(err, "Unable to record an operation")
		}
	}
	return nil
}

func (reporter *Reporter) addOperation(op *container.Operation) error {
	entry, err := reporter.getEntry(op)
	if err != nil {
		return err
	}
	if entry != nil && entry.Status == op.Status {
		log.Printf("Ignoring already-reported operation %s", op.Name)
		return nil
	}

	shouldReport := reporter.syncState == "incremental"
	if shouldReport {
		log.Printf("Notifying Slash with operation %#v", op)

		var title string
		if op.EndTime != "" {
			title = "Operation ended"
		} else {
			title = "Operation started"
		}
		message := slackhook.Message{}
		message.AddAttachment(&slackhook.Attachment{
			Title: title,
			Text:  op.OperationType,
		})

		ageThresholdForDisplayingTime := 3 * time.Minute

		var includeTime bool
		var startTime time.Time
		if t, err := time.Parse(time.RFC3339Nano, op.StartTime); err == nil {
			startTime = t
		} else {
			return err
		}
		includeTime = time.Since(startTime) > ageThresholdForDisplayingTime
		if !includeTime && op.EndTime != "" {
			if t, err := time.Parse(time.RFC3339Nano, op.EndTime); err == nil {
				includeTime = time.Since(t) > ageThresholdForDisplayingTime
			}
		}
		if includeTime {
			message.AddAttachment(&slackhook.Attachment{
				Title: "Since",
				Text:  humanize.Time(startTime),
			})
		}

		if op.Status != "" {
			var status string
			if op.StatusMessage != "" {
				status = fmt.Sprintf("%s — %s", op.Status, op.StatusMessage)
			} else {
				status = op.Status
			}
			if status != "" {
				message.AddAttachment(&slackhook.Attachment{
					Title: "Status",
					Text:  status,
				})
			}
		}
		if err := reporter.slackClient.Send(&message); err != nil {
			return SlackNotificationError{Err: err}
		}
	}

	return reporter.saveEntry(op)
}

func (reporter *Reporter) saveEntry(op *container.Operation) error {
	return reporter.db.Batch(func(tx *bolt.Tx) error {
		entry := Entry{
			Name:      op.Name,
			Zone:      op.Zone,
			ProjectID: reporter.options.ProjectID,
			Timestamp: time.Now(),
			Status:    op.Status,
		}
		b, err := json.Marshal(entry)
		if err != nil {
			return err
		}
		return tx.Bucket(bucketName).Put(reporter.keyForOperation(op), b)
	})
}

func (reporter *Reporter) keyForOperation(op *container.Operation) []byte {
	return []byte(fmt.Sprintf("%s--%s--%s", reporter.options.ProjectID, op.Zone, op.Name))
}

func (reporter *Reporter) getEntry(op *container.Operation) (*Entry, error) {
	var entry *Entry
	err := reporter.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketName).Get(reporter.keyForOperation(op))
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

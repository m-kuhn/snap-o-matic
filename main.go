package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"time"

	v3 "github.com/exoscale/egoscale/v3"
	"github.com/exoscale/egoscale/v3/credentials"
	"gopkg.in/yaml.v3" // YAML parsing using yaml.v3

	flag "github.com/spf13/pflag"
)

const (
	defaultEndpoint = v3.CHDk2
	marginFactor    = 0.1 // 10% margin for timeframe flexibility
)

type config struct {
	APIEndpoint     v3.Endpoint
	DryRun          bool
	Instances       []InstanceConfig // Multiple instances with retention policies
	CredentialsFile string
	LogLevel        string
}

type InstanceConfig struct {
	ID        v3.UUID           `yaml:"id"`
	Snapshots SnapshotRetention `yaml:"snapshots"`
}

type SnapshotRetention struct {
	Hourly  int `yaml:"hourly"`
	Daily   int `yaml:"daily"`
	Weekly  int `yaml:"weekly"`
	Monthly int `yaml:"monthly"`
	Yearly  int `yaml:"yearly"`
}

func exitWithErr(err error) {
	slog.Error("", "err", err)
	os.Exit(-1)
}

func main() {
	// Load config from YAML file
	cfg := config{
		APIEndpoint: getAPIEndpoint(), // Getting the API endpoint via the custom function
	}

	parseFlags(&cfg)

	if err := loadConfig("config.yaml", &cfg); err != nil {
		exitWithErr(err)
	}

	// Set log level
	switch cfg.LogLevel {
	case "debug":
		slog.SetLogLoggerLevel(slog.LevelDebug)
	case "error":
		slog.SetLogLoggerLevel(slog.LevelError)
	default:
		slog.SetLogLoggerLevel(slog.LevelInfo)
	}

	// Set up credentials
	var creds *credentials.Credentials
	if cfg.CredentialsFile != "" {
		var err error
		creds, err = apiCredentialsFromFile(cfg.CredentialsFile)
		if err != nil {
			exitWithErr(err)
		}
	} else {
		creds = credentials.NewEnvCredentials()
	}

	fmt.Println("Using endpoint: ", cfg.APIEndpoint)
	client, err := v3.NewClient(creds, v3.ClientOptWithEndpoint(cfg.APIEndpoint))
	if err != nil {
		exitWithErr(err)
	}

	ctx := context.Background()

	// Process each instance in the config
	for _, instance := range cfg.Instances {
		if err := processInstance(ctx, client, instance, cfg.DryRun); err != nil {
			exitWithErr(err)
		}
	}
}

func parseFlags(cfg *config) {
	flag.StringVarP(&cfg.CredentialsFile, "credentials-file", "f", "",
		"File to read API credentials from")

	flag.StringVarP(&cfg.LogLevel, "log-level", "L", "info", "Logging level, supported values: error,info,debug")
	flag.BoolVarP(&cfg.DryRun, "dry-run", "d", false, "Run in dry-run mode (read-only)")

	flag.ErrHelp = errors.New("") // Don't print "pflag: help requested" when the user invokes the help flags
	flag.Usage = func() {
		_, _ = fmt.Fprintln(os.Stderr, "snap-o-matic - Automatic Exoscale Compute instance volume snapshot")
		_, _ = fmt.Fprintln(os.Stderr, "")
		_, _ = fmt.Fprintln(os.Stderr, "*** WARNING ***")
		_, _ = fmt.Fprintln(os.Stderr, "")
		_, _ = fmt.Fprintln(os.Stderr, "This is experimental software and may not work as intended or may not be continued in the future. Use at your own risk.")
		_, _ = fmt.Fprintln(os.Stderr, "")
		_, _ = fmt.Fprintln(os.Stderr, "Usage:")
		flag.PrintDefaults()
		_, _ = fmt.Fprintf(os.Stderr, `
Supported environment variables:
  EXOSCALE_API_ENDPOINT    Exoscale Compute API endpoint (default %q)
  EXOSCALE_API_KEY         Exoscale API key
  EXOSCALE_API_SECRET      Exoscale API secret

API credentials file format:
  Instead of reading Exoscale API credentials from environment variables, it
  is possible to read those from a file formatted such as:

    api_key=EXOabcdef0123456789abcdef01
    api_secret=AbCdEfGhIjKlMnOpQrStUvWxYz-0123456789aBcDef
`, defaultEndpoint)
	}

	flag.Parse()
}

// Load the YAML configuration file
func loadConfig(filename string, cfg *config) error {
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	decoder := yaml.NewDecoder(file)
	return decoder.Decode(cfg)
}

// Process a specific instance by creating snapshots and managing retention
func processInstance(ctx context.Context, client *v3.Client, instance InstanceConfig, dryRun bool) error {
	fmt.Printf("Processing instance: %s\n", instance.ID)

	// Create a new snapshot for the instance
	snapshotID, err := createSnapshot(ctx, client, instance.ID, dryRun)
	if err != nil {
		return err
	}
	fmt.Printf("  Created snapshot: %s\n", snapshotID)

	// Get and manage snapshots based on retention policies
	snapshots, err := getSnapshots(ctx, client, instance.ID)
	if err != nil {
		return err
	}

	// Step 1: Categorize snapshots into their respective retention slots
	retainedSnapshots := categorizeSnapshots(snapshots, instance.Snapshots)

	// Step 2: Delete snapshots that were not retained
	cleanupSnapshots(ctx, client, snapshots, retainedSnapshots, dryRun)

	return nil
}

// Create a new snapshot for an instance
func createSnapshot(ctx context.Context, client *v3.Client, instanceID v3.UUID, dryRun bool) (v3.UUID, error) {
	if dryRun {
		fmt.Println("Dry run: Would create snapshot.")
		return "dry-run-snapshot-id", nil
	} else {
		fmt.Println("Creating snapshot for", instanceID)
	}

	snapshot, err := client.CreateSnapshot(ctx, instanceID)
	if err != nil {
		return "", err
	}

	return snapshot.ID, nil
}

// Retrieve existing snapshots for an instance
func getSnapshots(ctx context.Context, client *v3.Client, instanceID v3.UUID) ([]v3.Snapshot, error) {
	// Use the correct request type for listing snapshots
	/*req := &v3.SnapshotListRequest{
		InstanceID: instanceID,
	}*/

	snapshots, err := client.ListSnapshots(ctx)
	if err != nil {
		return nil, err
	}

	instanceSnapshots := []v3.Snapshot{}

	for _, snapshot := range snapshots.Snapshots {
		if snapshot.Instance.ID == instanceID {
			instanceSnapshots = append(instanceSnapshots, snapshot)
		}
	}

	return instanceSnapshots, nil
}

// Categorize snapshots into hourly, daily, weekly, etc. slots and return the list of retained snapshots
func categorizeSnapshots(snapshots []v3.Snapshot, retention SnapshotRetention) map[string]struct{} {
	// Sort snapshots by creation date (newest first)
	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].CreatedAT.After(snapshots[j].CreatedAT)
	})

	// Track retained snapshots by ID
	retainedSnapshots := make(map[string]struct{})

	// Define the timeframes
	timeframes := []struct {
		duration time.Duration
		limit    int
	}{
		{time.Hour, retention.Hourly},
		{24 * time.Hour, retention.Daily},
		{7 * 24 * time.Hour, retention.Weekly},
		{30 * 24 * time.Hour, retention.Monthly},
		{365 * 24 * time.Hour, retention.Yearly},
	}

	// Iterate through timeframes and retain snapshots
	for _, timeframe := range timeframes {
		retainForTimeframe(snapshots, timeframe.duration, timeframe.limit, retainedSnapshots)
	}

	return retainedSnapshots
}

// Retain snapshots for a specific timeframe and update the map of retained snapshots
func retainForTimeframe(snapshots []v3.Snapshot, timeframe time.Duration, limit int, retainedSnapshots map[string]struct{}) {
	margin := time.Duration(float64(timeframe) * marginFactor) // 10% margin
	var lastRetained time.Time
	retainedCount := 0

	fmt.Printf("Retaining snapshots for %s\n", timeframe)

	for _, snapshot := range snapshots {
		if _, exists := retainedSnapshots[snapshot.ID.String()]; exists {
			continue // Skip if this snapshot is already retained
		}

		created := snapshot.CreatedAT
		if lastRetained.IsZero() || created.Before(lastRetained.Add(-timeframe+margin)) {
			// Retain this snapshot if it doesn't violate the minimum distance rule
			lastRetained = created
			retainedSnapshots[snapshot.ID.String()] = struct{}{}
			fmt.Printf("  Retaining %s (%s)\n", snapshot.ID, snapshot.CreatedAT)
			retainedCount++

			if retainedCount >= limit {
				break
			}
		}
	}
}

// Cleanup snapshots that were not retained
func cleanupSnapshots(ctx context.Context, client *v3.Client, snapshots []v3.Snapshot, retainedSnapshots map[string]struct{}, dryRun bool) {
	for _, snapshot := range snapshots {
		// If the snapshot was not retained, delete it
		if _, retained := retainedSnapshots[snapshot.ID.String()]; !retained {
			deleteSnapshot(ctx, client, snapshot, dryRun)
		}
	}
}

// Delete a snapshot
func deleteSnapshot(ctx context.Context, client *v3.Client, snapshot v3.Snapshot, dryRun bool) {
	if dryRun {
		fmt.Printf("Dry run: Snapshot %s would be deleted\n", snapshot.ID)
	} else {
		op, err := client.DeleteSnapshot(ctx, snapshot.ID)
		if err != nil {
			fmt.Printf("Error deleting snapshot %s: %s\n", snapshot.ID, err)
		} else {
			_, err = client.Wait(ctx, op, v3.OperationStateSuccess)
			if err != nil {
				fmt.Printf("Error deleting snapshot: %s\n", err)
			} else {
				fmt.Printf("Deleted snapshot: %s\n", snapshot.ID)
			}
		}
	}
}

// Get the API endpoint
func getAPIEndpoint() v3.Endpoint {
	endpoint := os.Getenv("EXOSCALE_API_ENDPOINT")
	if endpoint == "" {
		return defaultEndpoint // default to predefined endpoint
	}
	return v3.Endpoint(endpoint)
}

// apiCredentialsFromFile parses a file containing the API credentials.
func apiCredentialsFromFile(path string) (*credentials.Credentials, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("unable to open credentials file: %w", err)
	}
	defer f.Close()

	apiKey := ""
	apiSecret := ""

	s := bufio.NewScanner(f)
	lineNr := 0
	for s.Scan() {
		if err := s.Err(); err != nil {
			return nil, fmt.Errorf("unable to parse credentials file: %w", err)
		}
		lineNr++
		line := s.Text()

		parts := strings.Split(line, "=")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid credentials line format on line %d (expected key=value)", lineNr)
		}
		k, v := parts[0], parts[1]

		switch strings.ToLower(k) {
		case "api_key":
			apiKey = v

		case "api_secret":
			apiSecret = v

		default:
			return nil, fmt.Errorf("invalid credentials file key on line %d", lineNr)
		}
	}

	return credentials.NewStaticCredentials(apiKey, apiSecret), nil
}

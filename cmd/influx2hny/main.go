package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	honeycomb "github.com/dstrelau/telegraf-honeycomb"
	"github.com/spf13/pflag"

	"github.com/honeycombio/libhoney-go"
)

var (
	// sets libhoney APIKey
	flagAPIKey = pflag.StringP("api-key", "k", "", "Honeycomb API Key (required)")

	// sets libhoney Dataset
	flagDataset = pflag.StringP("dataset", "d", "telegraf", `Honeycomb dataset to send to (default: "telegraf")`)

	// sets libhoney APIHost
	flagAPIHost = pflag.StringP("api-host", "h", "https://api.honeycomb.io/", `Honeycomb API host (default: "https://api.honeycomb.io/")`)

	// see Output.UnprefixedTags
	flagUnprefixedTags = pflag.StringSliceP("unprefixed-tags", "t", nil, "List of tags to NOT prefix with metric name when constructing Honeycomb field key (comma-separated; default: none)")

	// sets Output.DebugWriter = os.Stdout
	flagDebug = pflag.Bool("debug", false, "Enable debug logging on STDOUT (if running inside telegraf, you'll also want to run `telegraf --debug`)")
)

func main() {
	if err := pflag.CommandLine.MarkHidden("api-host"); err != nil {
		panic("BUG: host flag is gone")
	}
	pflag.Parse()

	if *flagAPIKey == "" {
		pflag.Usage()
		os.Exit(1)
	}

	cfg := libhoney.ClientConfig{
		APIKey:  *flagAPIKey,
		Dataset: *flagDataset,
	}
	if *flagAPIHost != "" {
		cfg.APIHost = *flagAPIHost
	}

	client, err := libhoney.NewClient(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create libhoney Client: %s", err.Error())
	}
	o := honeycomb.NewOutput(client)

	if flagUnprefixedTags != nil && len(*flagUnprefixedTags) > 0 {
		o.UnprefixedTags = *flagUnprefixedTags
	}
	if *flagDebug {
		o.DebugWriter = os.Stdout
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigint := make(chan os.Signal, 1)
	signal.Notify(sigint, syscall.SIGINT)
	go func() {
		<-sigint
		cancel()
	}()

	if err := o.Process(ctx, os.Stdin); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err.Error())
	}
	o.Close()
}

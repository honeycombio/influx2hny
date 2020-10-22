package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/pflag"

	"github.com/honeycombio/influx2hny"
	"github.com/honeycombio/libhoney-go"
)

func main() {
	var (
		flagHelp = pflag.BoolP("help", "h", false, "Display help and exit")

		// set libhoney config values
		flagAPIKey  = pflag.StringP("api-key", "k", "", "Honeycomb API Key (required)")
		flagDataset = pflag.StringP("dataset", "d", "telegraf", `Honeycomb dataset to send to (default: "telegraf")`)
		flagAPIHost = pflag.String("api-host", "https://api.honeycomb.io/", `Honeycomb API host (default: "https://api.honeycomb.io/")`)

		// see Output.UnprefixedTags
		flagUnprefixedTags = pflag.StringSliceP("unprefixed-tags", "t", nil, "List of tags to NOT prefix with metric name when constructing Honeycomb field key (comma-separated; default: none)")

		// sets Output.DebugWriter = os.Stdout
		flagDebug = pflag.Bool("debug", false, "Enable debug logging on STDOUT (if running inside telegraf, you'll also want to run `telegraf --debug`)")
	)

	if err := pflag.CommandLine.MarkHidden("api-host"); err != nil {
		// the --api-key flag was renamed but this check was not?
		panic(err)
	}

	pflag.Parse()

	if *flagHelp || *flagAPIKey == "" {
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
	o := influx2hny.NewOutput(client)

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
}

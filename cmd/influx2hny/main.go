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

var BuildVersion string

func main() {
	var (
		flagHelp    = pflag.BoolP("help", "h", false, "Display help and exit")
		flagVersion = pflag.BoolP("version", "V", false, "Print version and exit")

		// set libhoney config values
		flagAPIKey  = pflag.StringP("api-key", "k", "", "Honeycomb API Key (required)")
		flagDataset = pflag.StringP("dataset", "d", "telegraf", `Honeycomb dataset to send to`)
		flagAPIHost = pflag.String("api-host", "https://api.honeycomb.io/", `Honeycomb API host`)

		// see Output.UnprefixedTags
		flagUnprefixedTags = pflag.StringSliceP("unprefixed-tags", "t", nil, "List of tags to NOT prefix with metric name when constructing Honeycomb field key (comma-separated)")

		// sets Output.DebugWriter = os.Stdout
		flagDebug = pflag.Bool("debug", false, "Enable debug logging on STDOUT")
	)

	pflag.Parse()

	if *flagHelp {
		pflag.Usage()
		os.Exit(1)
	}
	if *flagVersion {
		if BuildVersion != "" {
			fmt.Println(BuildVersion)
		} else {
			fmt.Println("Ad-hoc")
		}
		os.Exit(1)
	}

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
	client.Close()
}

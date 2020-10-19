package main

import (
	"github.com/spf13/pflag"
)

var (
	flagAPIKey  = pflag.StringP("api-key", "k", "", "Honeycomb API Key (required)")
	flagDataset = pflag.StringP("dataset", "d", "telegraf", `Honeycomb dataset to send to (default: "telegraf")`)
	flagHost    = pflag.StringP("host", "h", "https://api.honeycomb.io/", `Honeycomb API host (default: "https://api.honeycomb.io/")`)

	// By default, every field and tag of a Metric are sent as a Honeycomb
	// field prefixed by the metric name. So a telegraf Metric like this:
	// { name=disk // tags={device:sda} fields={free:232827793}}
	// becomes two Honeycomb fields: "disk.device" and "disk.free".
	//
	// Exclude tags from this behavor by setting them in this list.
	// Any global tags should be included here. The "host" tag will always be
	// treated as if it is included in this list (ie, it is always sent as the
	// unprefixed field "host")
	flagSpecialTags = pflag.StringSliceP("unprefixed-tags", "t", nil, "List of tags to NOT prefix with metric name when constructing Honeycomb field key (comma-separated; default: none)")
)

func main() {
	pflag.CommandLine.MarkHidden("host")
	pflag.Parse()
	pflag.Usage()
}

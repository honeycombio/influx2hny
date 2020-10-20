# influx2hny

influx2hny reads influx-formatted metrics on STDIN and sends them to a
Honeycomb dataset. It is meant to be used as an `execd` output plugin for
telegraf.

## Installation

`go install github.com/honeycombio/influx2hny/cmd/influx2hny`

## Usage with Telegraf

```
[[ outputs.execd ]]

command = ["influx2hny", "--dataset", "System Metrics", "--api-key", "$HONEYCOMB_API_KEY"]

data_format = "influx"
```

## Developing

`go run ./cmd/influx2hny` should work.

`inflix2hny` also supports a `--debug` flag that will enable debug logging to STDOUT.

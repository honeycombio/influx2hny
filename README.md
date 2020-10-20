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

## Metric Aggregation (Flattening)

influx2hny performs automatic flattening of metrics into some fewer number of
Honeycomb events, in the best case just a single event per timestamp with all
metric values. (Since Honeycomb pricing plans depend on the number of events you
send, this can help keep these metrics from consuming a large portion of your
ingest quota.)

A Telegraf metric has a name plus a number of fields and tags. The line protocol
looks like this:

```
system,host=myhost load1=0.14,load5=0.04,load15=0.01,n_cpus=16i 1603224500000000000
system,host=myhost uptime=94600i 1603224500000000000
swap,host=myhost free=6295912448i,used_percent=16.2353515625,total=7516192768i,used=1220280320i 1603224500000000000
```

You can see two `system` metrics and a single `swap` metric with the tag
`host=myhost` on all of them. The first metric has fields `load1, load5,
load15, n_cpus` while there is just `uptime` on the second and several others
on the `swap` metric.

By default, influx2hny attempts to combine all these metrics by creating unique
Honeycomb fields from the tags and fields, prefixed with the metric name. In
the above example, we would create one event with fields like `system.load1`,
`system.update`, and `swap.free`. As long as all these fields are unique, they
will all be added to a single Honeycomb event.

Tags are processed the same way, so the following metric could be combined with
those above, adding a field `diskio.name` along with event fields for all the
other metric fields like `diskio.reads`.

```
diskio,host=myhost,name=sda reads=20138i,writes=12193i,read_bytes=239178752i,iops_in_progress=0i,merged_reads=38256i,merged_writes=344693i,write_bytes=5723148288i,read_time=12667i,write_time=21396i,io_time=11750i,weighted_io_time=31420i 1603224500000000000
```

For this flattening to succeed though, the fields and tags must be distinct. If
we also received the following metric with the same timestamp, the `diskio`
metrics would need to be sent as two separate Honeycomb events because the
`name` tag differs:

```
diskio,host=myhost,name=sdb weighted_io_time=570700i,merged_reads=214i,reads=278338i,write_bytes=9092214784i,read_time=88961i,write_time=732109i,io_time=86540i,iops_in_progress=0i,merged_writes=439157i,writes=141179i,read_bytes=3721647104i 1603224500000000000
```

Other metrics would still be flattened, so given all the metrics in this
example were sent together, the two `diskio` metrics would be sent plus one
flattened metric with `system` and `swap` for a total of 3 Honeycomb events.

Note that the `host` tag is treated specially: it is always sent un-prefixed as
simply `host`. If you use Telegraf `global_tags`, you may want to add
additional tags to this list of special tags that are never prefixed. Use `-t
tag1,tag2` (aka `--unprefixed-tags`) to do this.

## Developing

`go run ./cmd/influx2hny` should work.

influx2hny also supports a `--debug` flag that will enable debug logging to STDOUT.

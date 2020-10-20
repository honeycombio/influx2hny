package influx2hny

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"sync"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/parsers/influx"
	"golang.org/x/sync/errgroup"

	"github.com/honeycombio/libhoney-go"
)

type Output struct {
	// FlushInterval controls the time that the Output will buffer Metrics
	// before attempting to flatten them into a single Honeycomb event per
	// timestamp. See the documentation for Aggregate for more discussion.
	//
	// Default: 5 seconds.
	FlushInterval time.Duration

	// MaxBufferSize is the maximum number of Metrics we'll hold onto in memory
	// before initiating a flush.
	//
	// Default: 1000
	MaxBufferSize int

	// DebugWriter has debug log messages written to it if set.
	// Useful for debugging usage inside of telegraf.
	//
	// Default: ioutil.Discard
	DebugWriter io.Writer

	// UnprefixedTags is the list of tags that will NOT be prefixed by the
	// associated metric name when constructing the Honeycomb field name.
	//
	// By default, every field and tag of a Metric are sent as a Honeycomb
	// field prefixed by the metric name. So a telegraf Metric like this:
	// { name=disk // tags={device:sda} fields={free:232827793}}
	// becomes two Honeycomb fields: "disk.device" and "disk.free".
	//
	// Exclude tags from this behavior by setting them in this list.
	// Any global tags should be included here.
	//
	// The "host" tag will always be treated as if it is included in this list.
	// (i.e., it is always sent as the field "host")
	UnprefixedTags []string

	hnyClient    *libhoney.Client
	influxParser *influx.Parser

	// metrics is used to shovel between Read and Aggregate.
	metrics chan (telegraf.Metric)

	// buffer is owned by the Aggregate routine, but needs to be locked
	// to avoid duplicate flushes.
	mx     sync.Mutex
	buffer []telegraf.Metric
}

// NewOutput returns an Output with default configuration.
//
// Public fields can be updated with new configuration, but must be changed
// before calling Process().
func NewOutput(hnyClient *libhoney.Client) *Output {
	return &Output{
		FlushInterval: 5 * time.Second,
		MaxBufferSize: 1000,
		DebugWriter:   ioutil.Discard,

		metrics:      make(chan telegraf.Metric),
		hnyClient:    hnyClient,
		influxParser: influx.NewParser(influx.NewMetricHandler()),
	}
}

func (o *Output) debug(s string, args ...interface{}) {
	fmt.Fprintf(o.DebugWriter, "[influx2hny] "+s, args...)
}

func (o *Output) Process(ctx context.Context, r io.Reader) error {
	group, ctx := errgroup.WithContext(ctx)
	group.Go(func() error { return o.Read(ctx, r) })
	group.Go(func() error { return o.Aggregate(ctx) })
	return group.Wait()
}

func (o *Output) Read(ctx context.Context, r io.Reader) error {
	var (
		s   = bufio.NewScanner(r)
		m   telegraf.Metric
		err error
	)
	// for every line on the reader (usually os.Stdin), parse it in Influx format
	// and shovel it onto the metrics channel
	for s.Scan() {
		m, err = o.influxParser.ParseLine(s.Text())
		if err != nil {
			o.debug("failed to parse metric: %s\n", err.Error())
			continue
		}
		o.debug("msg=metric.parsed name=%s fields=%d\n", m.Name(), len(m.FieldList()))
		o.metrics <- m
	}
	return nil
}

// Aggregate converts Telegraf metrics into Honeycomb events.
//
// It reads off the Output's metrics channel and attempts to flatten the
// metrics into as few Honeycomb events as possible.
//
// Runs indefinitely until the passed Context is canceled or an unrecoverable
// error occurs.
func (o *Output) Aggregate(ctx context.Context) error {
	flushTick := time.NewTicker(o.FlushInterval)
	for {
		select {
		case <-ctx.Done():
			o.Flush()
			return nil
		case m := <-o.metrics:
			o.mx.Lock()
			o.buffer = append(o.buffer, m)
			size := len(o.buffer)
			o.mx.Unlock()

			if size >= o.MaxBufferSize {
				o.debug("msg=buffer.max_size size=")
				o.Flush()
			}
		case <-flushTick.C:
			// FIXME: we have a bit of a race condition here.
			//
			// Telegraf collects and outputs on an interval, so we fill the
			// buffer (above) before processing the flush. Usually, this means
			// the flush will be during a period of no input and catch all
			// metrics for a timestamp.
			//
			// However, it's still possible that if very busy we flush without
			// having all metrics for a timestamp in our buffer. This isn't
			// terrible though; it just creates an extra Honeycomb event or two
			// on the next interval. Fixing this would require implementing a
			// priority queue-like structure where we could only flush metrics
			// that were at least T time old, which doesn't seem worth it.
			o.Flush()
		}
	}
}

func (o *Output) Flush() {
	o.debug("msg=flush.begin metrics=%d\n", len(o.buffer))

	o.mx.Lock()
	eventsCount := 0
	defer func() {
		o.debug("msg=flush.end metrics=%d events=%d\n", len(o.buffer), eventsCount)
		o.hnyClient.Flush()
		o.buffer = nil
		o.mx.Unlock()
	}()

	// For each timestamp, we want to send a single event to Honeycomb with as
	// many metrics as possible. However, some metrics may be sent to us more
	// than once. Eg, disk usage is sent once for each disk. So build a
	// map[time]map[name][]Metric. We'll then look at all the metric names for
	// a given timestamp that can be combined and flatten them into a single
	// event. Metrics that have non-mergable fields will be sent as separate
	// events.
	//
	// (We group by metric name because each metric's fields are prefixed by
	// the metric name before they're sent. So any metrics with different names
	// are definitely mergeable. This also gives us a nice way to know which
	// metrics to send separately.)
	metricsByTimeAndName := make(map[time.Time]map[string][]telegraf.Metric)

	for _, m := range o.buffer {
		metricsByName := metricsByTimeAndName[m.Time()]
		if metricsByName == nil {
			metricsByName = make(map[string][]telegraf.Metric)
		}
		metricsByName[m.Name()] = append(metricsByName[m.Name()], m)
		metricsByTimeAndName[m.Time()] = metricsByName
	}

	for ts, metricsByName := range metricsByTimeAndName {
		// The one event for all the metrics in this timestamp that can be flattened
		flatEvent := o.hnyClient.NewEvent()
		flatEvent.Timestamp = ts
		eventsCount++

		// For each metric, check if it's mergeable (a single event or disjoint
		// fields with the same tags). If it is, it can go in the flatEvent.
		// Otherwise, create and send unique event for it.
		for name, metrics := range metricsByName {
			// if the metrics with the same name result in a distinct set of field names,
			// we can still flatten them.
			if mergeable(metrics) {
				o.debug("msg=metrics.flatten name=%s count=%d timestamp=%d\n", name, len(metrics), ts.Unix())
				for i := range metrics {
					if err := flatEvent.Add(o.dataForMetric(metrics[i])); err != nil {
						o.debug("libhoney Add error: %s\n", err.Error())
					}
				}
			} else {
				o.debug("msg=metrics.individual name=%s count=%d timestamp=%d\n", name, len(metrics), ts.Unix())
				for i := range metrics {
					ev := o.hnyClient.NewEvent()
					ev.Timestamp = ts
					eventsCount++
					if err := ev.Add(o.dataForMetric(metrics[i])); err != nil {
						o.debug("libhoney Add error: %s\n", err.Error())
					} else if err := ev.Send(); err != nil {
						o.debug("libhoney Send error: %s\n", err.Error())
					}
				}
			}
		}

		// once we've aggregated everything for that timestamp, send the flattened event.
		if err := flatEvent.Send(); err != nil {
			o.debug("libhoney Send error: %s\n", err.Error())
		}
	}
}

func (o *Output) dataForMetric(m telegraf.Metric) map[string]interface{} {
	data := make(map[string]interface{})

	// add tags, by default prefixed with the metric name
	// do not prefix the special tag "host" or any tag listed in "special tags"
	for _, t := range m.TagList() {
		k := m.Name() + "." + t.Key
		if t.Key == "host" {
			k = t.Key
		} else {
			for i := range o.UnprefixedTags {
				if o.UnprefixedTags[i] == t.Key {
					k = t.Key
				}
			}
		}

		data[k] = t.Value
	}

	// add each field and value prefixed by metric / measurement name to data payload
	for _, f := range m.FieldList() {
		data[m.Name()+"."+f.Key] = f.Value
	}
	return data
}

// mergeable returns true if the metrics can be merged into a single Honeycomb event
// without losing information. Specifically, this means that the metrics have
// disjoint fields and the same list of tags.
func mergeable(ms []telegraf.Metric) bool {
	var (
		fields = make(map[string]struct{})
		tags   []*telegraf.Tag
	)

	for i := range ms {
		if tags == nil {
			// Use the first set of tags we see as the canonical list to match
			tags = ms[i].TagList()
		} else {
			// Docs say that this returns ordered, so comparing without sorting
			// should be safe.
			for j, t := range ms[i].TagList() {
				if tags[j].Key != t.Key || tags[j].Value != t.Value {
					return false
				}
			}
		}

		// Check we've never seen any of the fields before.
		// Fields are unique by their metric name & field key, as we'll
		// concatenate the two when constructing the honeycomb event.
		for _, f := range ms[i].FieldList() {
			k := ms[i].Name() + f.Key
			if _, exists := fields[k]; exists {
				return false
			}
			fields[k] = struct{}{}
		}
	}

	return true
}

// Close ensures libhoney is closed.
func (o *Output) Close() error {
	o.hnyClient.Flush()
	o.hnyClient.Close()
	return nil
}

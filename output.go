package influx2hny

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/influxdata/telegraf"
	"golang.org/x/sync/errgroup"

	"github.com/honeycombio/libhoney-go"
)

// MetricsReader parses Metrics from the Reader and sends them to the channel.
type MetricsReader interface {
	Read(context.Context, io.Reader, chan<- telegraf.Metric) error
}

// EventAggregator transforms []telegraf.Metric -> []*libhoney.Event
type EventAggregator interface {
	Aggregate([]telegraf.Metric) []*libhoney.Event
}

type Output struct {
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

	// MaxBufferSize is the maximum number of Metrics we'll hold onto in memory
	// before initiating a flush.
	//
	// Default: 1000
	MaxBufferSize int

	// HoneycombClient is used to construct and send evetns to Honeycomb.
	// If unset, will use libhoney globals instead.
	HoneycombClient *libhoney.Client

	// Reader parses messages into Metrics.
	// Default: &InfluxReader{}
	Reader MetricsReader

	// Aggregator transforms Metrics into Events.
	// Default: &FewestEventsAggregator{}
	Aggregator EventAggregator

	// FlushSignal can used to signal the Output to flush the buffer.
	//
	// Eg, this can be set to a channel that you never send anything to in
	// order to disable automatic flushing (and only flush when the buffer is
	// full), or time.Ticker to automatically flush on an interval.
	//
	// Default: a 5 second ticker.
	FlushSignal <-chan time.Time

	mx     sync.Mutex
	buffer []telegraf.Metric
}

// Process coordinates the goroutines necessary for parsing Metrics from the
// Reader, aggregating them, and sending them to Honeycomb.
//
// Should not be called concurrently.
func (o *Output) Process(ctx context.Context, r io.Reader) error {
	metrics := make(chan telegraf.Metric)
	if o.MaxBufferSize == 0 {
		o.MaxBufferSize = 1000
	}
	if o.Reader == nil {
		o.Reader = &InfluxReader{}
	}
	if o.Aggregator == nil {
		o.Aggregator = &FewestEventsAggregator{}
	}
	if o.FlushSignal == nil {
		tick := time.NewTicker(5 * time.Second)
		o.FlushSignal = tick.C
		defer func() {
			o.FlushSignal = nil
			tick.Stop()
		}()
	}
	if o.HoneycombClient == nil {
		// FIXME: use libhoney globals
	}

	// goroutine 1: read from STDIN and put Metrics on channel
	// goroutine 2: buffer from channel, flushing to libhoney on timer
	group, ctx := errgroup.WithContext(ctx)
	group.Go(func() error { return o.Reader.Read(ctx, r, metrics) })
	group.Go(func() error { return o.process(ctx, metrics) })
	err := group.Wait()

	// cleanup
	close(metrics)
	o.Flush()
	return err
}

// process buffers Metrics from the channel, then aggregates them and sends
// them to libhoney on a ticker.
//
// Runs until the context is canceled.
func (o *Output) process(ctx context.Context, in <-chan telegraf.Metric) error {
	for {
		select {
		case <-ctx.Done():
			o.Flush()
			return nil
		case m := <-in:
			o.mx.Lock()
			o.buffer = append(o.buffer, m)
			size := len(o.buffer)
			o.mx.Unlock()

			if size >= o.MaxBufferSize {
				o.Flush()
			}
		case <-o.FlushSignal:
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
	o.mx.Lock()
	defer func() {
		o.HoneycombClient.Flush()
		o.buffer = nil
		o.mx.Unlock()
	}()

	for _, ev := range o.Aggregator.Aggregate(o.buffer) {
		_ = ev.Send()
	}
}

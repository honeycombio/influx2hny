package honeycomb

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/parsers/influx"

	"github.com/honeycombio/libhoney-go"
)

type Output struct {
	hnyClient      *libhoney.Client
	influxParser   *influx.Parser
	metrics        chan (telegraf.Metric)
	unprefixedTags []string
}

type Config struct {
	APIKey         string
	Dataset        string
	APIHost        string
	UnprefixedTags []string
}

var DefaultConfig = Config{
	Dataset: "telegraf",
	APIHost: "https://api.honeycomb.io",
}

func NewOutput(c Config) (*Output, error) {
	switch "" {
	case c.APIKey:
		return nil, errors.New("APIKey is required")
	case c.APIHost:
		return nil, errors.New("APIHost is required")
	case c.Dataset:
		c.Dataset = "telegraf"
	}

	client, err := libhoney.NewClient(libhoney.ClientConfig{
		APIKey:  c.APIKey,
		Dataset: c.Dataset,
		APIHost: c.APIHost,
	})
	if err != nil {
		return nil, err
	}

	p := influx.NewParser(influx.NewMetricHandler())

	return &Output{
		hnyClient:      client,
		influxParser:   p,
		unprefixedTags: c.UnprefixedTags,
	}, nil
}

func (o *Output) Process(ctx context.Context, r io.Reader) error {
	// TODO: read metrics channel into aggregation buckets
	// TODO: then flush to libhoney on timer
	// TODO: handle context cancelation
	return o.Read(ctx, r)
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
			fmt.Fprintf(os.Stderr, "failed to parse metric: %s\n", err.Error())
			continue
		}
		o.metrics <- m
	}
	return nil
}

func (o *Output) Write(metrics []telegraf.Metric) error {
	evs, err := o.BuildEvents(metrics)
	if err != nil {
		return fmt.Errorf("Honeycomb event creation error: %s", err.Error())
	}

	for _, ev := range evs {
		fmt.Printf("ev = %+v\n", ev)
		// send event
		if err = ev.Send(); err != nil {
			return fmt.Errorf("Honeycomb Send error: %s", err.Error())
		}
	}

	libhoney.Flush()

	return nil
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
			for i := range o.unprefixedTags {
				if o.unprefixedTags[i] == t.Key {
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

func (o *Output) BuildEvents(ms []telegraf.Metric) ([]*libhoney.Event, error) {
	// For each timestamp, we want to send a single event to Honeycomb with as
	// many metrics as possible. However, some metrics may be sent to us more
	// than once. Eg, disk usage is sent once for each disk. So build a
	// map[time]map[name][]Metric. We'll then look at all the metric names for
	// a given timestamp that have only one value and batch them. Any metrics
	// that have > 1 value will be sent as separate events.
	metricsByTimeAndName := make(map[time.Time]map[string][]telegraf.Metric)

	for _, m := range ms {
		metricsByName := metricsByTimeAndName[m.Time()]
		if metricsByName == nil {
			metricsByName = make(map[string][]telegraf.Metric)
		}
		metricsByName[m.Name()] = append(metricsByName[m.Name()], m)
		metricsByTimeAndName[m.Time()] = metricsByName
	}

	var evs []*libhoney.Event
	for ts, metricsByName := range metricsByTimeAndName {
		// the single event for all the metrics flattened into a single event
		flatEvent := libhoney.NewEvent()
		flatEvent.Timestamp = ts

		// for each metric name with only 1 Metric, flatten it.
		// otherwise, create a unique event for it.
		for name, metrics := range metricsByName {
			if len(metrics) == 1 {
				fmt.Println("merging one:", name)
				if err := flatEvent.Add(o.dataForMetric(metrics[0])); err != nil {
					return nil, err
				}
			} else {
				fmt.Println("sending many:", name)

				// if the metrics with the same name result in a distinct set of field names,
				// we can still flatten them.
				if mergeable(metrics) {
					fmt.Println("yay! mergeable:", name)
					for i := range metrics {
						if err := flatEvent.Add(o.dataForMetric(metrics[i])); err != nil {
							return nil, err
						}
					}
				} else {
					for i := range metrics {
						ev := libhoney.NewEvent()
						ev.Timestamp = ts
						if err := ev.Add(o.dataForMetric(metrics[i])); err != nil {
							return nil, err
						}
						evs = append(evs, ev)
					}
				}
			}
		}

		// once we've processed all the events for this timestamp, we can add the flat event to the batch
		evs = append(evs, flatEvent)
	}

	return evs, nil
}

// mergeable returns true if the metrics can be merged into a single Honeycomb event
// without losing information. Specifically, this means that the metrics have
// disjoint fields an the same list of tags.
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
		// concantenate the two when constructing the honeycomb event.
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
func (h *Output) Close() error {
	libhoney.Close()
	return nil
}

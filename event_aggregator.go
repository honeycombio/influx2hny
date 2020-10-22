package influx2hny

import (
	"time"

	"github.com/influxdata/telegraf"

	"github.com/honeycombio/libhoney-go"
)

// FewestEventsAggregator groups Metrics into the fewest possible Events.
type FewestEventsAggregator struct {
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

	// NewEvent is the function called to construct a new libhoney.Event.
	// By default this is set to libhoney.NewEvent, but it can be overridden,
	// eg. to the NewEvent function of a non-global libhoney.Client.
	NewEvent func() *libhoney.Event

	// OnEventAddError is called when an error happens while adding metric data
	// to a Honeycomb event. This shouldn't really happen, as this is usually
	// due to a type error when calling event.Add() and all Metrics should be
	// numeric, which should be fine.
	//
	// By default, this is a no-op, but it can be used to, eg, log the error or
	// cancel the Read context causing the reader to stop completely.
	OnEventAddError func(error)
}

// Aggregate groups Metrics into as few unique events as possible per timestamp.
//
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
func (a *FewestEventsAggregator) Aggregate(metrics []telegraf.Metric) []*libhoney.Event {
	metricsByTimeAndName := make(map[time.Time]map[string][]telegraf.Metric)

	for _, m := range metrics {
		metricsByName := metricsByTimeAndName[m.Time()]
		if metricsByName == nil {
			metricsByName = make(map[string][]telegraf.Metric)
		}
		metricsByName[m.Name()] = append(metricsByName[m.Name()], m)
		metricsByTimeAndName[m.Time()] = metricsByName
	}

	var evs []*libhoney.Event

	for ts, metricsByName := range metricsByTimeAndName {
		// The one event for all the metrics in this timestamp that can be flattened
		flatEvent := a.NewEvent()
		flatEvent.Timestamp = ts

		// For each metric, check if it's mergeable (a single event or disjoint
		// fields with the same tags). If it is, it can go in the flatEvent.
		// Otherwise, create and send unique event for it.
		for _, metrics := range metricsByName {
			if mergeable(metrics) {
				for i := range metrics {
					if err := flatEvent.Add(a.dataForMetric(metrics[i])); err != nil {
						if a.OnEventAddError != nil {
							a.OnEventAddError(err)
						}
					}
				}
			} else {
				for i := range metrics {
					ev := a.NewEvent()
					ev.Timestamp = ts
					if err := ev.Add(a.dataForMetric(metrics[i])); err != nil {
						if a.OnEventAddError != nil {
							a.OnEventAddError(err)
						}
					} else {
						evs = append(evs, ev)
					}
				}
			}
		}

		// once we've aggregated everything for that timestamp, add the flattened event.
		evs = append(evs, flatEvent)
	}

	return evs
}

// dataForMetric returns the Event data for the Metric.

// This consists of both tags and fields, with the Event field names prefixed
// by the Metric's name unless included in UnprefixedTags.
func (a *FewestEventsAggregator) dataForMetric(m telegraf.Metric) map[string]interface{} {
	data := make(map[string]interface{})

	// add tags, by default prefixed with the metric name
	// do not prefix the special tag "host" or any tag listed in "special tags"
	for _, t := range m.TagList() {
		k := m.Name() + "." + t.Key
		if t.Key == "host" {
			k = t.Key
		} else {
			for i := range a.UnprefixedTags {
				if a.UnprefixedTags[i] == t.Key {
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

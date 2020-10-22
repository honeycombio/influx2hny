package influx2hny

import (
	"bufio"
	"context"
	"io"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/parsers/influx"
)

// MetricParser turns strings into telegraf.Metrics.
type MetricParser interface {
	ParseLine(string) (telegraf.Metric, error)
}

// InfluxReader reads Influx Line Protocol formatted messages.
type InfluxReader struct {
	// Parser is what parses the lines of text into telegraf.Metrics.
	// Default: an appropriately configured *influx.Parser
	Parser MetricParser

	// OnParseError is called when an error happens while parsing a metric.
	// By default, this is a no-op, but it can be used to, eg, log the error or
	// cancel the Read context causing the reader to stop completely.
	OnParseError func(error)
}

// Read reads lines from the Reader until EOF, parsing each into a
// telegraf.Metric then sending the Metric to the channel.
//
// Runs until reading returns EOF or the context is canceled.
//
// Should not be called concurrently.
func (i *InfluxReader) Read(ctx context.Context, r io.Reader, out chan<- (telegraf.Metric)) error {
	if i.Parser == nil {
		i.Parser = influx.NewParser(influx.NewMetricHandler())
	}

	var (
		s   = bufio.NewScanner(r)
		m   telegraf.Metric
		err error
	)

	for s.Scan() {
		select {
		case <-ctx.Done():
			return nil
		default:
			m, err = i.Parser.ParseLine(s.Text())
			if err != nil {
				if i.OnParseError != nil {
					i.OnParseError(err)
				}
				continue
			}
			out <- m
		}
	}
	return s.Err()
}

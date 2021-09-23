package influx2hny

import (
	"testing"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/metric"
	"github.com/stretchr/testify/assert"

	"github.com/honeycombio/libhoney-go"
	"github.com/honeycombio/libhoney-go/transmission"
)

type tWriter struct {
	T *testing.T
}

func (w tWriter) Write(b []byte) (int, error) {
	w.T.Logf("%s", b)
	return len(b), nil
}

func newTestOutput(t *testing.T) (*Output, *transmission.MockSender) {
	tx := &transmission.MockSender{}
	c, err := libhoney.NewClient(libhoney.ClientConfig{
		Transmission: tx,
	})
	o := &Output{
		hnyClient:   c,
		DebugWriter: tWriter{T: t},
	}
	assert.NoError(t, err)
	return o, tx
}

func TestFlush(t *testing.T) {
	t.Run("handles metrics with an extra tag", func(t *testing.T) {
		now := time.Now()
		tags1 := map[string]string{
			"host": "kafka-01",
		}
		tags2 := map[string]string{
			"host": "kafka-01",
			"role": "kafka",
		}
		fields := map[string]interface{}{
			"usage_idle": float64(99),
			"usage_busy": float64(1),
		}
		m1, err := metric.New("cpu", tags1, fields, now)
		assert.NoError(t, err)
		m2, err := metric.New("cpu", tags2, fields, now)
		assert.NoError(t, err)

		o, _ := newTestOutput(t)
		o.buffer = []telegraf.Metric{m1, m2}
		o.Flush() // works and does not panic
	})
}

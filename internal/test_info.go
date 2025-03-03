package internal

import (
	"math"
	"sort"
	"time"
)

// TestInfo holds information about what specification was used
// to perform the test and results of the test.
type TestInfo struct {
	Spec   Spec
	Result Results
}

// Header represents HTTP header.
type Header struct {
	Key, Value string
}

// Spec contains information about test performed.
type Spec struct {
	NumberOfConnections uint64

	TestType         TestType
	NumberOfRequests uint64
	TestDuration     time.Duration

	Method string
	URL    string

	Headers []Header

	Body         string
	BodyFilePath string

	CertPath string
	KeyPath  string

	Stream     bool
	Timeout    time.Duration
	ClientType ClientType

	Rate *uint64
}

// IsTimedTest tells if the test was limited by time.
func (s Spec) IsTimedTest() bool {
	return s.TestType == ByTime
}

// IsTestWithNumberOfReqs tells if the test was limited by the number
// of requests.
func (s Spec) IsTestWithNumberOfReqs() bool {
	return s.TestType == ByNumberOfReqs
}

// IsFastHTTP tells whether fasthttp were used as HTTP client to
// perform the test.
func (s Spec) IsFastHTTP() bool {
	return s.ClientType == FastHTTP
}

// IsNetHTTPV1 tells whether Go's default net/http library and
// HTTP/1.x were used to perform the test.
func (s Spec) IsNetHTTPV1() bool {
	return s.ClientType == NetHTTP1
}

// IsNetHTTPV2 tells whether Go's default net/http library and
// HTTP/1.x (or HTTP/2.0, if possible) were used to perform the test.
func (s Spec) IsNetHTTPV2() bool {
	return s.ClientType == NetHTTP2
}

// Results holds results of the test.
type Results struct {
	BytesRead, BytesWritten int64
	TimeTaken               time.Duration

	Req1XX, Req2XX, Req3XX, Req4XX, Req5XX, Req502 uint64
	Others                                         uint64
	StatusCodes map[int]uint64

	Errors []ErrorWithCount

	Latencies ReadonlyUint64Histogram
	Requests  ReadonlyFloat64Histogram
}

// ReadonlyUint64Histogram is a readonly histogram with uint64 keys
type ReadonlyUint64Histogram interface {
	Get(uint64) uint64
	VisitAll(func(uint64, uint64) bool)
	Count() uint64
}

// ReadonlyFloat64Histogram is a readonly histogram with float64 keys
type ReadonlyFloat64Histogram interface {
	Get(float64) uint64
	VisitAll(func(float64, uint64) bool)
	Count() uint64
}

// Throughput returns total throughput (read + write) in bytes per
// second
func (r Results) Throughput() float64 {
	return float64(r.BytesRead+r.BytesWritten) / r.TimeTaken.Seconds()
}

// LatenciesStats contains statistical information about latencies.
type LatenciesStats struct {
	// These are in microseconds
	Mean   float64
	Stddev float64
	Max    float64

	// This is  map[0.0 <= p <= 1.0 (percentile)]microseconds
	Percentiles map[float64]uint64
}

// LatenciesStats performs various statistical calculations on
// latencies.
func (r Results) LatenciesStats(percentiles []float64) *LatenciesStats {
	h := r.Latencies
	sum := uint64(0)
	count := uint64(0)
	max := uint64(0)
	pairs := make([]struct{ k, v uint64 }, 0, h.Count())

	// Gather all the data
	h.VisitAll(func(f uint64, c uint64) bool {
		if f > max {
			max = f
		}
		sum += f * c
		count += c
		pairs = append(pairs, struct{ k, v uint64 }{f, c})
		return true
	})
	if count < 1 {
		return nil
	}

	// Calculate percentiles
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].k < pairs[j].k
	})
	percentilesMap := map[float64]uint64{}
	for _, pc := range percentiles {
		if _, calculated := percentilesMap[pc]; calculated {
			continue
		}
		if pc < 0 || pc > 1 {
			// Drop percentiles outside of [0, 1] range
			continue
		}
		rank := uint64(pc*float64(count) + 0.5)
		total := uint64(0)
		for _, p := range pairs {
			total += p.v
			if total >= rank {
				percentilesMap[pc] = p.k
				break
			}
		}
	}

	// Calculate mean and standard deviation
	mean := float64(sum) / float64(count)
	sumOfSquares := float64(0)
	h.VisitAll(func(f uint64, c uint64) bool {
		sumOfSquares += math.Pow(float64(f)-mean, 2)
		return true
	})
	stddev := 0.0
	if count > 2 {
		stddev = math.Sqrt(sumOfSquares / float64(count))
	}
	return &LatenciesStats{
		Mean:   mean,
		Stddev: stddev,
		Max:    float64(max),

		Percentiles: percentilesMap,
	}
}

// RequestsStats contains statistical information about requests.
type RequestsStats struct {
	// These are in requests per second.
	Mean   float64
	Stddev float64
	Max    float64

	// This is  map[0.0 <= p <= 1.0 (percentile)](req-s per second)
	Percentiles map[float64]float64
}

// RequestsStats performs various statistical calculations on
// latencies.
func (r Results) RequestsStats(percentiles []float64) *RequestsStats {
	h := r.Requests
	sum := float64(0)
	count := uint64(0)
	max := float64(0)
	pairs := make([]struct {
		k float64
		v uint64
	}, 0, h.Count())

	// Gather all the data
	h.VisitAll(func(f float64, c uint64) bool {
		if math.IsInf(f, 0) || math.IsNaN(f) {
			return true
		}
		if f > max {
			max = f
		}
		sum += f * float64(c)
		count += c
		pairs = append(pairs, struct {
			k float64
			v uint64
		}{f, c})
		return true
	})
	if count < 1 {
		return nil
	}

	// Calculate percentiles
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].k < pairs[j].k
	})
	percentilesMap := map[float64]float64{}
	for _, pc := range percentiles {
		if _, calculated := percentilesMap[pc]; calculated {
			continue
		}
		if pc < 0 || pc > 1 {
			// Drop percentiles outside of [0, 1] range
			continue
		}
		rank := uint64(pc*float64(count) + 0.5)
		total := uint64(0)
		for _, p := range pairs {
			total += p.v
			if total >= rank {
				percentilesMap[pc] = p.k
				break
			}
		}
	}

	// Calculate mean and standard deviation
	mean := sum / float64(count)
	sumOfSquares := float64(0)
	h.VisitAll(func(f float64, c uint64) bool {
		if math.IsInf(f, 0) || math.IsNaN(f) {
			return true
		}
		sumOfSquares += math.Pow(f-mean, 2)
		return true
	})
	stddev := 0.0
	if count > 2 {
		stddev = math.Sqrt(sumOfSquares / float64(count))
	}
	return &RequestsStats{
		Mean:   mean,
		Stddev: stddev,
		Max:    max,

		Percentiles: percentilesMap,
	}
}

// ErrorWithCount contains error description alongside with number of
// times this error occurred.
type ErrorWithCount struct {
	Error string
	Count uint64
}

// TestType represents the type of test that were performed.
type TestType int

const (
	_ TestType = iota
	// ByTime is a test limited by durations.
	ByTime
	// ByNumberOfReqs is a test limited by number of requests
	// performed.
	ByNumberOfReqs
)

// ClientType is the type of HTTP client used in test
type ClientType int

const (
	// FastHTTP is fasthttp's HTTP client
	FastHTTP ClientType = iota
	// NetHTTP1 is Go's default HTTP client with forced HTTP/1.x
	NetHTTP1
	// NetHTTP2 is Go's default HTTP client with HTTP/2.0 permitted.
	NetHTTP2
)

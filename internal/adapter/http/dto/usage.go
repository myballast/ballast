package dto

import "time"

// Usage is what was moved over a period.
//
// Four volumes rather than two, because they are two measurements of two
// different things. The egress pair was counted on the wire by the proxy every
// run's traffic goes through: TLS records, headers and all, which is what an
// infrastructure bill is made of. The data pair is what the generators
// themselves reported, which is smaller — their difference is the overhead this
// installation pays.
type Usage struct {
	// From is included, To is not.
	From time.Time `json:"from"`
	To   time.Time `json:"to"`

	EgressBytes       int64 `json:"egress_bytes"`
	IngressBytes      int64 `json:"ingress_bytes"`
	DataSentBytes     int64 `json:"data_sent_bytes"`
	DataReceivedBytes int64 `json:"data_received_bytes"`

	// Runs that ended inside the period, and how many of those nobody could
	// measure. A total with no idea how much of it is missing is a total
	// nobody can price.
	Runs           int `json:"runs"`
	UnmeasuredRuns int `json:"unmeasured_runs"`
}

package domain

// Threshold is a pass or fail rule a scenario imposes on one k6 metric.
//
// Only the two fields the generated k6 options need are modelled here; the rest
// of the entity belongs to the scenario use cases and arrives with them.
type Threshold struct {
	// Metric is the k6 metric the rule applies to, tag selector included:
	// http_req_duration{name:POST /login} narrows it to one endpoint.
	Metric string
	// Expression is the k6 threshold expression, such as p(95)<300 or rate<0.01.
	Expression string
}

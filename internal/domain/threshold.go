package domain

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// Threshold is a pass or fail rule a scenario imposes on one k6 metric.
type Threshold struct {
	ID uuid.UUID
	// Metric is the k6 metric the rule applies to, tag selector included:
	// http_req_duration{name:POST /login} narrows it to one endpoint.
	Metric string
	// Expression is the k6 threshold expression, such as p(95)<300 or rate<0.01.
	Expression string
}

const (
	maxMetricLength     = 200
	maxExpressionLength = 120
)

// ValidateThreshold refuses a rule Ballast will not store.
//
// What the expression means is k6's to judge: it owns the comparison, and the
// verdict of a run is read from its summary rather than recomputed. What is
// checked here is only that there is something to hand it.
func ValidateThreshold(metric, expression string) error {
	switch {
	case strings.TrimSpace(metric) == "":
		return fmt.Errorf("%w: the metric is empty", ErrInvalidThreshold)
	case len(metric) > maxMetricLength:
		return fmt.Errorf("%w: the metric is longer than %d characters",
			ErrInvalidThreshold, maxMetricLength)
	case strings.TrimSpace(expression) == "":
		return fmt.Errorf("%w: the expression is empty", ErrInvalidThreshold)
	case len(expression) > maxExpressionLength:
		return fmt.Errorf("%w: the expression is longer than %d characters",
			ErrInvalidThreshold, maxExpressionLength)
	}
	return nil
}

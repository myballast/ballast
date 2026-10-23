package handler

import (
	"reflect"
	"testing"

	"github.com/youssefmahersi/ballast/internal/adapter/http/dto"
	"github.com/youssefmahersi/ballast/internal/domain"
)

// fill sets every field of a struct to a non-zero value, so that a field the
// translation forgets comes back as zero.
func fill(t *testing.T, value reflect.Value) {
	t.Helper()

	for i := range value.NumField() {
		field := value.Field(i)
		switch field.Kind() {
		case reflect.String:
			field.SetString("x")
		case reflect.Bool:
			field.SetBool(true)
		case reflect.Int:
			field.SetInt(3)
		case reflect.Float64:
			field.SetFloat(1.5)
		case reflect.Map:
			field.Set(reflect.ValueOf(map[string]string{"k": "v"}))
		case reflect.Slice:
			item := reflect.New(field.Type().Elem()).Elem()
			fill(t, item)
			field.Set(reflect.Append(reflect.MakeSlice(field.Type(), 0, 1), item))
		default:
			t.Fatalf("fill does not know the kind %s of field %s", field.Kind(), value.Type().Field(i).Name)
		}
	}
}

// Every field of a node and an edge survives the round trip through the API
// shape. A field added to the domain and forgotten in the copy fails here
// rather than being silently dropped on every save.
func TestTheGraphSurvivesTheRoundTripThroughItsDTO(t *testing.T) {
	var node domain.Node
	fill(t, reflect.ValueOf(&node).Elem())
	var edge domain.Edge
	fill(t, reflect.ValueOf(&edge).Elem())

	graph := domain.ScenarioGraph{Start: "x", Nodes: []domain.Node{node}, Edges: []domain.Edge{edge}}

	shown := graphDTO(&graph)
	if !reflect.DeepEqual(graphOf(shown), &graph) {
		t.Fatalf("round trip lost something:\n got %+v\nwant %+v", graphOf(shown), &graph)
	}

	// And the DTO has no field the domain lacks.
	if reflect.TypeOf(dto.GraphNode{}).NumField() != reflect.TypeOf(domain.Node{}).NumField() {
		t.Error("dto.GraphNode and domain.Node have a different number of fields")
	}
	if reflect.TypeOf(dto.GraphEdge{}).NumField() != reflect.TypeOf(domain.Edge{}).NumField() {
		t.Error("dto.GraphEdge and domain.Edge have a different number of fields")
	}
}

func TestNoGraphStaysNoGraph(t *testing.T) {
	if graphDTO(nil) != nil || graphOf(nil) != nil {
		t.Error("a missing graph became a graph")
	}
}

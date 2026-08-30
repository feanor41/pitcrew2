package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/fmazzalomo/pitcrew/internal/history"
)

type unifiedLoader struct {
	fakeLoader
	deliveries []history.Delivery
	results    []history.DeliverySearchResult
	details    map[string]history.DeliveryDetail
	calls      *[]string
}

func (f unifiedLoader) ListDeliveries(context.Context) ([]history.Delivery, error) {
	*f.calls = append(*f.calls, "list")
	return f.deliveries, nil
}

func (f unifiedLoader) GetDelivery(_ context.Context, id string) (history.DeliveryDetail, error) {
	*f.calls = append(*f.calls, "get:"+id)
	return f.details[id], nil
}

func (f unifiedLoader) SearchDeliveries(_ context.Context, query string) ([]history.DeliverySearchResult, error) {
	*f.calls = append(*f.calls, "search:"+query)
	return f.results, nil
}

func TestUnifiedDeliveriesUseSharedReadPathsAndThinDirectDetail(t *testing.T) {
	calls := []string{}
	direct := history.Delivery{ID: "dl-123", Revision: 2, Route: "direct_inline", Status: "blocked", Goal: "Fix the small thing", RouteReason: "One well-understood file", Summary: "Waiting for input", NextAction: "Ask the user", CreatedAt: "2026-08-29T12:00:00Z", UpdatedAt: "2026-08-29T12:05:00Z"}
	workflow := history.Delivery{ID: "wf-123", Revision: 7, Route: history.FullWorkflow, Status: "implementing", Goal: "Build the large thing"}
	loader := unifiedLoader{deliveries: []history.Delivery{direct, workflow}, results: []history.DeliverySearchResult{{DeliveryID: direct.ID, Route: direct.Route, Status: direct.Status, Context: "Waiting for input"}}, details: map[string]history.DeliveryDetail{direct.ID: {Delivery: direct}}, calls: &calls}

	model := New(loader)
	model, _ = model.Update(model.Init()())
	model.screen = WorkflowsScreen
	model, _ = model.Update(textKey("/"))
	model.search.SetValue("blocked")
	model, command := model.Update(special(tea.KeyEnter))
	model, _ = model.Update(command())
	if model.screen != ResultsScreen || len(model.deliveryResults) != 1 {
		t.Fatalf("unified search state = %#v", model)
	}
	model, command = model.Update(textKey("r"))
	model, _ = model.Update(command())
	model, _ = model.Update(special(tea.KeyEscape))
	model.selected = 0
	model, command = model.Update(special(tea.KeyEnter))
	model, _ = model.Update(command())
	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	frame := ansi.Strip(model.View().Content)
	for _, want := range []string{"DIRECT DELIVERY", "Route  direct_inline", "Status  blocked", "Waiting for input", "Ask the user"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("direct detail missing %q:\n%s", want, frame)
		}
	}
	for _, absent := range []string{"Explore", "Spec", "Design", "Units"} {
		if strings.Contains(frame, absent) {
			t.Fatalf("direct detail invented %q:\n%s", absent, frame)
		}
	}
	model, command = model.Update(textKey("r"))
	model, _ = model.Update(command())
	if got := strings.Join(calls, ","); got != "list,search:blocked,search:blocked,get:dl-123,get:dl-123" {
		t.Fatalf("read calls = %q", got)
	}
}

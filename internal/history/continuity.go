package history

import (
	"context"

	"github.com/fmazzalomo/pitcrew/internal/delivery"
)

type ActiveCandidate struct {
	DeliveryID string `json:"delivery_id"`
	Kind       string `json:"kind"`
	Revision   int64  `json:"revision"`
	Status     string `json:"status"`
	NextAction string `json:"next_action"`
}

type ActiveContinuity struct {
	Count      int               `json:"count"`
	Candidates []ActiveCandidate `json:"candidates"`
	NextAction string            `json:"-"`
	Deliveries []Delivery        `json:"-"`
}

func EmptyActiveContinuity() ActiveContinuity {
	return ActiveContinuity{Candidates: []ActiveCandidate{}, Deliveries: []Delivery{}, NextAction: "aion admit new delivery"}
}

func (s *Service) ActiveContinuity(ctx context.Context) (ActiveContinuity, error) {
	deliveries, err := s.ListActiveDeliveries(ctx)
	if err != nil {
		return ActiveContinuity{}, err
	}
	result := EmptyActiveContinuity()
	result.Count, result.Deliveries = len(deliveries), deliveries
	for i, item := range deliveries {
		kind := "direct_delivery"
		nextAction := item.NextAction
		if item.Route == FullWorkflow {
			kind = "workflow"
		} else if item.InspectedRevision == item.Revision {
			nextAction = delivery.UpdateCommand(item.ID, item.Revision)
		} else {
			nextAction = "delivery show --delivery-id " + item.ID
		}
		result.Deliveries[i].NextAction = nextAction
		result.Candidates = append(result.Candidates, ActiveCandidate{item.ID, kind, item.Revision, item.Status, nextAction})
	}
	if result.Count == 1 {
		candidate := result.Candidates[0]
		result.NextAction = "delivery show --delivery-id " + candidate.DeliveryID
		if candidate.Kind == "direct_delivery" && result.Deliveries[0].InspectedRevision == candidate.Revision {
			result.NextAction = candidate.NextAction
		}
	}
	if result.Count > 1 {
		result.NextAction = "aion clarify delivery identity"
	}
	return result, nil
}

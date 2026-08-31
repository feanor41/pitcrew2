package history

import "context"

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
	for _, delivery := range deliveries {
		kind := "direct_delivery"
		if delivery.Route == FullWorkflow {
			kind = "workflow"
		}
		result.Candidates = append(result.Candidates, ActiveCandidate{delivery.ID, kind, delivery.Revision, delivery.Status, delivery.NextAction})
	}
	if result.Count == 1 {
		result.NextAction = "delivery show --delivery-id " + result.Candidates[0].DeliveryID
	}
	if result.Count > 1 {
		result.NextAction = "aion clarify delivery identity"
	}
	return result, nil
}

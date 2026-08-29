package correction

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/fmazzalomo/pitcrew/internal/plan"
)

type Authority string

const (
	AuthorityNone       Authority = "none"
	AuthorityAutomatic  Authority = "automatic"
	AuthorityAuthorized Authority = "authorized"
)

type Projection struct {
	PolicyAware     bool      `json:"policy_aware"`
	Allowed         int       `json:"allowed"`
	Used            int       `json:"used"`
	BlockerRevision int64     `json:"blocker_revision,omitempty"`
	BlockerContent  string    `json:"blocker_content,omitempty"`
	Authority       Authority `json:"authority"`
	NextAction      string    `json:"next_action"`
}

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

type correctionFact struct {
	artifactID              int64
	acceptedRevision        int64
	aggregateReviewRevision int64
	authorizationArtifactID int64
}

type authorizationFact struct {
	artifactID              int64
	acceptedRevision        int64
	aggregateReviewRevision int64
}

// Project derives reusable correction authority without mutating stored facts.
// ordinaryNextAction is supplied by the lifecycle owner to avoid a package cycle.
func Project(ctx context.Context, db queryer, workflowID, ordinaryNextAction string) (Projection, error) {
	var state, body string
	if err := db.QueryRowContext(ctx, `SELECT w.state,p.body FROM workflows w JOIN plans p ON p.workflow_id=w.id WHERE w.id=?`, workflowID).Scan(&state, &body); err != nil {
		return Projection{}, err
	}
	var acceptedPlan plan.Plan
	if err := json.Unmarshal([]byte(body), &acceptedPlan); err != nil {
		return Projection{}, fmt.Errorf("decode accepted plan: %w", err)
	}
	if err := plan.Validate(acceptedPlan); err != nil {
		return Projection{}, fmt.Errorf("validate accepted plan: %w", err)
	}
	result := Projection{
		PolicyAware: acceptedPlan.HasAggregateCorrectionPolicy(),
		Allowed:     acceptedPlan.AggregateCorrectionPolicy.AutomaticRounds,
		Authority:   AuthorityNone,
		NextAction:  ordinaryNextAction,
	}

	rows, err := db.QueryContext(ctx, `SELECT id,kind,content,accepted_revision FROM artifacts WHERE workflow_id=? AND kind IN ('aggregate_review','aggregate_correction','correction_authorization') ORDER BY accepted_revision,id`, workflowID)
	if err != nil {
		return Projection{}, err
	}
	defer rows.Close()
	type reviewFact struct {
		revision int64
		content  string
	}
	var reviews []reviewFact
	var corrections []correctionFact
	var authorizations []authorizationFact
	for rows.Next() {
		var id, revision int64
		var kind, content string
		if err = rows.Scan(&id, &kind, &content, &revision); err != nil {
			return Projection{}, err
		}
		switch kind {
		case "aggregate_review":
			var value struct {
				Verdict string `json:"verdict"`
			}
			if json.Unmarshal([]byte(content), &value) == nil && value.Verdict == "corrections" {
				reviews = append(reviews, reviewFact{revision: revision, content: content})
			}
		case "aggregate_correction":
			var value struct {
				AggregateReviewRevision int64  `json:"aggregate_review_revision"`
				Authority               string `json:"authority"`
				AuthorizationArtifactID int64  `json:"authorization_artifact_id"`
			}
			if json.Unmarshal([]byte(content), &value) == nil && value.AggregateReviewRevision > 0 && (value.Authority == string(AuthorityAutomatic) || value.Authority == string(AuthorityAuthorized)) {
				corrections = append(corrections, correctionFact{id, revision, value.AggregateReviewRevision, value.AuthorizationArtifactID})
			}
		case "correction_authorization":
			var value struct {
				AggregateReviewRevision int64 `json:"aggregate_review_revision"`
				UserDirectionConfirmed  bool  `json:"user_direction_confirmed"`
			}
			if json.Unmarshal([]byte(content), &value) == nil && value.AggregateReviewRevision > 0 && value.UserDirectionConfirmed {
				authorizations = append(authorizations, authorizationFact{id, revision, value.AggregateReviewRevision})
			}
		}
	}
	if err = rows.Err(); err != nil {
		return Projection{}, err
	}

	result.Used = len(corrections)
	legacyRows, err := db.QueryContext(ctx, `SELECT at FROM activities WHERE workflow_id=? AND action='unit_aggregate_recovered' ORDER BY at,id`, workflowID)
	if err != nil {
		return Projection{}, err
	}
	var legacyTimes []string
	for legacyRows.Next() {
		var at string
		if err = legacyRows.Scan(&at); err != nil {
			legacyRows.Close()
			return Projection{}, err
		}
		legacyTimes = append(legacyTimes, at)
	}
	if err = legacyRows.Close(); err != nil {
		return Projection{}, err
	}
	result.Used += len(legacyTimes)
	legacyEventRows, err := db.QueryContext(ctx, `SELECT revision_after,at FROM events WHERE workflow_id=? AND reason='aggregate_corrections' ORDER BY at,revision_after`, workflowID)
	if err != nil {
		return Projection{}, err
	}
	type legacyEvent struct {
		revision int64
		at       string
		used     bool
	}
	var legacyEvents []legacyEvent
	for legacyEventRows.Next() {
		var event legacyEvent
		if err = legacyEventRows.Scan(&event.revision, &event.at); err != nil {
			legacyEventRows.Close()
			return Projection{}, err
		}
		legacyEvents = append(legacyEvents, event)
	}
	if err = legacyEventRows.Close(); err != nil {
		return Projection{}, err
	}
	var legacyRecoveryRevisions []int64
	for _, at := range legacyTimes {
		for i := range legacyEvents {
			if !legacyEvents[i].used && legacyEvents[i].at == at {
				legacyEvents[i].used = true
				legacyRecoveryRevisions = append(legacyRecoveryRevisions, legacyEvents[i].revision)
				break
			}
		}
	}
	legacyResolved := map[int64]bool{}
	for _, recoveryRevision := range legacyRecoveryRevisions {
		for i := len(reviews) - 1; i >= 0; i-- {
			if reviews[i].revision < recoveryRevision && !legacyResolved[reviews[i].revision] {
				legacyResolved[reviews[i].revision] = true
				break
			}
		}
	}
	for i := len(reviews) - 1; i >= 0; i-- {
		resolved := legacyResolved[reviews[i].revision]
		for _, fact := range corrections {
			if fact.aggregateReviewRevision == reviews[i].revision && fact.acceptedRevision > reviews[i].revision {
				resolved = true
				break
			}
		}
		if !resolved {
			result.BlockerRevision, result.BlockerContent = reviews[i].revision, reviews[i].content
			break
		}
	}

	terminal := state == "completed" || state == "abandoned"
	if terminal {
		result.NextAction = "none"
		return result, nil
	}
	if result.BlockerRevision == 0 {
		if state == "ready_to_complete" {
			result.NextAction = "workflow complete"
		}
		return result, nil
	}
	if result.Used < result.Allowed {
		result.Authority, result.NextAction = AuthorityAutomatic, "workflow recover-aggregate"
		return result, nil
	}
	for i := len(authorizations) - 1; i >= 0; i-- {
		authorization := authorizations[i]
		if authorization.aggregateReviewRevision != result.BlockerRevision || authorization.acceptedRevision <= result.BlockerRevision || authorizationConsumed(authorization, corrections) {
			continue
		}
		result.Authority, result.NextAction = AuthorityAuthorized, "workflow recover-aggregate"
		return result, nil
	}
	result.NextAction = "user authorization required"
	return result, nil
}

func authorizationConsumed(authorization authorizationFact, corrections []correctionFact) bool {
	for _, fact := range corrections {
		if fact.authorizationArtifactID == authorization.artifactID && fact.acceptedRevision > authorization.acceptedRevision {
			return true
		}
	}
	return false
}

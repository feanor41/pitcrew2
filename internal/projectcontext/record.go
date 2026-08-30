package projectcontext

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var ErrRecordUnavailable = errors.New("project context recording is unavailable")
var ErrLegacyBlocked = errors.New("project context recording requires legacy consolidation")

const LegacyRecoveryNextAction = "run pitcrew project inspect, then pitcrew project consolidate for the exact candidate set"

type LegacyGate func(context.Context) error

type Committer func(context.Context, Record, string, time.Time) (inspection Inspection, persisted bool, err error)

type RecordTarget struct {
	CheckoutRoot string
	GateLegacy   LegacyGate
	Commit       Committer
}

type RecordResolver func(context.Context) (RecordTarget, error)

type Recorder interface {
	Record(context.Context, Record, string, time.Time) (Inspection, bool, error)
}

type recorder struct {
	resolve RecordResolver
}

func NewRecorder(resolve RecordResolver) Recorder { return &recorder{resolve: resolve} }

type RecoveryError struct {
	NextAction string
	Err        error
}

func (e *RecoveryError) Error() string {
	return fmt.Sprintf("%v: %v", ErrLegacyBlocked, e.Err)
}

func (e *RecoveryError) Unwrap() error { return e.Err }

func (e *RecoveryError) Is(target error) bool {
	return target == ErrLegacyBlocked || errors.Is(e.Err, target)
}

func (s *recorder) Record(ctx context.Context, candidate Record, actor string, at time.Time) (Inspection, bool, error) {
	if err := Validate(candidate); err != nil {
		return Inspection{}, false, err
	}
	if err := ValidateActor(actor); err != nil {
		return Inspection{}, false, err
	}
	if s.resolve == nil {
		return Inspection{}, false, ErrRecordUnavailable
	}
	target, err := s.resolve(ctx)
	if err != nil {
		return Inspection{}, false, err
	}
	if err := ConfineEvidence(target.CheckoutRoot, candidate); err != nil {
		return Inspection{}, false, err
	}
	if target.GateLegacy == nil || target.Commit == nil {
		return Inspection{}, false, ErrRecordUnavailable
	}
	if err := target.GateLegacy(ctx); err != nil {
		if errors.Is(err, ErrLegacyBlocked) {
			return Inspection{}, false, &RecoveryError{NextAction: LegacyRecoveryNextAction, Err: err}
		}
		return Inspection{}, false, err
	}
	return target.Commit(ctx, CloneRecord(candidate), actor, at)
}

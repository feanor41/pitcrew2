package cli

import (
	"context"
	"errors"
	"time"

	"github.com/fmazzalomo/pitcrew/internal/project"
	"github.com/fmazzalomo/pitcrew/internal/projectcontext"
	"github.com/fmazzalomo/pitcrew/internal/sddinitializer"
	"github.com/fmazzalomo/pitcrew/internal/store"
)

func runContext(args []string, deps Dependencies) int {
	if equalArgs(args, "--help") {
		writeHelp(deps.Stdout, "Usage: pitcrew context <inspect|initialize|record> [options]\n")
		return 0
	}
	if len(args) == 2 && args[1] == "--help" && (args[0] == "inspect" || args[0] == "initialize" || args[0] == "record") {
		writeHelp(deps.Stdout, "Usage: pitcrew context "+args[0]+" [options]\n")
		return 0
	}
	service := contextService(deps)
	ctx := context.Background()
	switch {
	case equalArgs(args, "inspect"):
		inspection, err := service.Inspect(ctx)
		return contextResult(deps, inspection, contextNext(inspection), err)
	case equalArgs(args, "initialize"):
		result, err := sddinitializer.Initialize(ctx, sddinitializer.Dependencies{Inspector: service, Recorder: contextRecorder(deps, service), Now: deps.Now})
		if err != nil {
			return contextFailure(deps, err)
		}
		next := "context record"
		if result.Inspection.Status == projectcontext.Complete {
			next = "workflow new"
		}
		return contextResult(deps, map[string]any{"inspection": result.Inspection, "persisted": result.Persisted}, next, nil)
	case len(args) > 0 && args[0] == "record":
		values, err := parseFlags(args[1:], flagRules{required: []string{"--actor", "--input-file"}})
		if err != nil {
			return fail(deps, err, err.Error())
		}
		record, err := decodeInputFile[projectcontext.Record](values.one("--input-file"))
		if err != nil {
			return fail(deps, err, err.Error())
		}
		inspection, _, err := contextRecorder(deps, service).Record(ctx, record, values.one("--actor"), deps.Now())
		return contextResult(deps, inspection, contextNext(inspection), err)
	default:
		return fail(deps, ErrUsage, "usage: pitcrew context <inspect|initialize|record>")
	}
}

func contextResult(deps Dependencies, data any, next string, err error) int {
	if err != nil {
		return contextFailure(deps, err)
	}
	if err := writeSuccess(deps, data, next); err != nil {
		return fail(deps, err, err.Error())
	}
	return 0
}

func contextFailure(deps Dependencies, err error) int {
	message := err.Error()
	var recovery *projectcontext.RecoveryError
	if errors.As(err, &recovery) {
		message += "; next action: " + recovery.NextAction
	}
	if isContextState(err) {
		return fail(deps, ErrState, message)
	}
	return fail(deps, err, message)
}

func isContextState(err error) bool {
	return errors.Is(err, project.ErrMigrationRequired) || errors.Is(err, project.ErrNotRepository) ||
		errors.Is(err, project.ErrUnsafeMetadata) || errors.Is(err, project.ErrUnsafePath) ||
		errors.Is(err, store.ErrInvalidState) || errors.Is(err, projectcontext.ErrInvalidRecord) ||
		errors.Is(err, projectcontext.ErrEvidenceOutsideCheckout) || errors.Is(err, projectcontext.ErrRecordUnavailable) ||
		errors.Is(err, projectcontext.ErrLegacyBlocked)
}

func contextNext(inspection projectcontext.Inspection) string {
	if inspection.Status == projectcontext.Complete {
		return "workflow new"
	}
	return "context initialize"
}

func contextService(deps Dependencies) *projectcontext.Service {
	return projectcontext.New(projectcontext.Dependencies{Resolve: func(ctx context.Context) (projectcontext.Resolved, error) {
		view, err := project.Inspect(deps.ProjectRoot, deps.DataHome)
		if err != nil {
			return projectcontext.Resolved{}, err
		}
		return projectcontext.Resolved{CheckoutRoot: view.Project.CheckoutRoot, Load: func(ctx context.Context) (projectcontext.Record, string, bool, error) {
			opened, err := store.OpenProjectReadOnly(ctx, view.Project, view.Paths)
			if err != nil || opened.State == store.Uninitialized {
				return projectcontext.Record{}, "", false, err
			}
			defer opened.Store.Close()
			snapshot, found, err := opened.Store.LoadProjectContext(ctx)
			return snapshot.Record, snapshot.UpdatedAt, found, err
		}}, nil
	}})
}

func contextRecorder(deps Dependencies, service *projectcontext.Service) projectcontext.Recorder {
	return projectcontext.NewRecorder(func(context.Context) (projectcontext.RecordTarget, error) {
		initial, err := project.Inspect(deps.ProjectRoot, deps.DataHome)
		if err != nil {
			return projectcontext.RecordTarget{}, err
		}
		return projectcontext.RecordTarget{CheckoutRoot: initial.Project.CheckoutRoot, GateLegacy: func(ctx context.Context) error {
			current, err := project.Inspect(deps.ProjectRoot, deps.DataHome)
			if err != nil {
				return err
			}
			opened, err := store.OpenProjectReadOnly(ctx, current.Project, current.Paths)
			if err == nil && opened.State == store.Initialized {
				defer opened.Store.Close()
				err = gateLegacy(opened.Store, current.Project.ID, current.Legacy)
			} else if err == nil {
				err = project.GateLegacy(current.Legacy, "")
			}
			if errors.Is(err, project.ErrMigrationRequired) {
				return projectcontext.ErrLegacyBlocked
			}
			return err
		}, Commit: func(ctx context.Context, record projectcontext.Record, actor string, at time.Time) (projectcontext.Inspection, bool, error) {
			current, err := project.Inspect(deps.ProjectRoot, deps.DataHome)
			if err != nil {
				return projectcontext.Inspection{}, false, err
			}
			opened, err := store.OpenProject(ctx, current.Project, current.Paths)
			if err != nil {
				return projectcontext.Inspection{}, false, err
			}
			changed, err := opened.ReplaceProjectContext(ctx, record, actor, at)
			_ = opened.Close()
			if err != nil {
				return projectcontext.Inspection{}, false, err
			}
			inspection, err := service.Inspect(ctx)
			return inspection, changed, err
		}}, nil
	})
}

package scheduler

import (
	"strconv"

	"github.com/hashicorp/nomad/nomad/structs"
)

const (
	AnnotationForcesCreate            = "forces create"
	AnnotationForcesDestroy           = "forces destroy"
	AnnotationForcesInplaceUpdate     = "forces in-place update"
	AnnotationForcesDestructiveUpdate = "forces create/destroy update"
)

// UpdateTypes denote the type of update to occur against the task group.
const (
	UpdateTypeIgnore            = "ignore"
	UpdateTypeCreate            = "create"
	UpdateTypeDestroy           = "destroy"
	UpdateTypeMigrate           = "migrate"
	UpdateTypeInplaceUpdate     = "in-place update"
	UpdateTypeDestructiveUpdate = "create/destroy update"
)

// Annotate takes the diff between the old and new version of a Job, the
// scheduler's plan annotations and will add annotations to the diff to aide
// human understanding of the plan.
//
// Currently the things that are annotated are:
// * Task group changes will be annotated with:
//    * Count up and count down changes
//    * Update counts (creates, destroys, migrates, etc)
// * Task changes will be annotated with:
//    * forces create/destroy update
//    * forces in-place update
func Annotate(diff *structs.JobDiff, annotations *structs.PlanAnnotations) error {
	// No annotation needed as the job was either just submitted or deleted.
	if diff.Type != structs.DiffTypeEdited {
		return nil
	}

	tgDiffs := diff.TaskGroups
	if len(tgDiffs) == 0 {
		return nil
	}

	for _, tgDiff := range tgDiffs {
		if err := annotateTaskGroup(tgDiff, annotations); err != nil {
			return err
		}
	}

	return nil
}

// annotateTaskGroup takes a task group diff and annotates it.
func annotateTaskGroup(diff *structs.TaskGroupDiff, annotations *structs.PlanAnnotations) error {
	// Don't annotate unless the task group was edited. If it was a
	// create/destroy it is not needed.
	if diff.Type != structs.DiffTypeEdited {
		return nil
	}

	// Annotate the updates
	if annotations != nil {
		tg, ok := annotations.DesiredTGUpdates[diff.Name]
		if ok {
			if diff.Updates == nil {
				diff.Updates = make(map[string]uint64, 6)
			}

			if tg.Ignore != 0 {
				diff.Updates[UpdateTypeIgnore] = tg.Ignore
			}
			if tg.Place != 0 {
				diff.Updates[UpdateTypeCreate] = tg.Place
			}
			if tg.Migrate != 0 {
				diff.Updates[UpdateTypeMigrate] = tg.Migrate
			}
			if tg.Stop != 0 {
				diff.Updates[UpdateTypeDestroy] = tg.Stop
			}
			if tg.InPlaceUpdate != 0 {
				diff.Updates[UpdateTypeInplaceUpdate] = tg.InPlaceUpdate
			}
			if tg.DestructiveUpdate != 0 {
				diff.Updates[UpdateTypeDestructiveUpdate] = tg.DestructiveUpdate
			}
		}
	}

	// Annotate the count
	if err := annotateCountChange(diff); err != nil {
		return err
	}

	// Annotate the tasks.
	taskDiffs := diff.Tasks
	if len(taskDiffs) == 0 {
		return nil
	}

	for _, taskDiff := range taskDiffs {
		annotateTask(taskDiff)
	}

	return nil
}

// annotateCountChange takes a task group diff and annotates the count
// parameter.
func annotateCountChange(diff *structs.TaskGroupDiff) error {
	// Don't annotate unless the task was edited. If it was a create/destroy it
	// is not needed.
	if diff.Type != structs.DiffTypeEdited {
		return nil
	}

	var countDiff *structs.FieldDiff
	for _, diff := range diff.Fields {
		if diff.Name == "Count" {
			countDiff = diff
			break
		}
	}

	// Didn't find
	if countDiff == nil {
		return nil
	}
	oldV, err := strconv.Atoi(countDiff.Old)
	if err != nil {
		return err
	}

	newV, err := strconv.Atoi(countDiff.New)
	if err != nil {
		return err
	}

	if oldV < newV {
		countDiff.Annotations = append(countDiff.Annotations, AnnotationForcesCreate)
	} else if newV < oldV {
		countDiff.Annotations = append(countDiff.Annotations, AnnotationForcesDestroy)
	}

	return nil
}

// annotateCountChange takes a task diff and annotates it.
func annotateTask(diff *structs.TaskDiff) {
	// Don't annotate unless the task was edited. If it was a create/destroy it
	// is not needed.
	if diff.Type != structs.DiffTypeEdited {
		return
	}

	// All changes to primitive fields result in a destructive update.
	destructive := false
	if len(diff.Fields) != 0 {
		destructive = true
	}

	// Changes that can be done in-place are log configs, services and
	// constraints.
	for _, oDiff := range diff.Objects {
		switch oDiff.Name {
		case "LogConfig", "Service", "Constraint":
			continue
		default:
			destructive = true
			break
		}
	}

	if destructive {
		diff.Annotations = append(diff.Annotations, AnnotationForcesDestructiveUpdate)
	} else {
		diff.Annotations = append(diff.Annotations, AnnotationForcesInplaceUpdate)
	}
}

package task

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/yusing/go-proxy/internal/common"
	E "github.com/yusing/go-proxy/internal/error"
	"github.com/yusing/go-proxy/internal/logging"
	F "github.com/yusing/go-proxy/internal/utils/functional"
)

var globalTask = createGlobalTask()

func createGlobalTask() (t *Task) {
	t = new(Task)
	t.name = "root"
	t.ctx, t.cancel = context.WithCancelCause(context.Background())
	t.subtasks = F.NewSet[*Task]()
	return
}

func testResetGlobalTask() {
	globalTask = createGlobalTask()
}

type (
	TaskStarter interface {
		// Start starts the object that implements TaskStarter,
		// and returns an error if it fails to start.
		//
		// The task passed must be a subtask of the caller task.
		//
		// callerSubtask.Finish must be called when start fails or the object is finished.
		Start(callerSubtask *Task) E.Error
	}
	TaskFinisher interface {
		// Finish marks the task as finished and cancel its context.
		//
		// Then call Wait to wait for all subtasks, OnFinished and OnSubtasksFinished
		// of the task to finish.
		//
		// Note that it will also cancel all subtasks.
		Finish(reason any)
	}
	// Task controls objects' lifetime.
	//
	// Objects that uses a Task should implement the TaskStarter and the TaskFinisher interface.
	//
	// When passing a Task object to another function,
	// it must be a sub-Task of the current Task,
	// in name of "`currentTaskName`Subtask"
	//
	// Use Task.Finish to stop all subtasks of the Task.
	Task struct {
		ctx    context.Context
		cancel context.CancelCauseFunc

		parent     *Task
		subtasks   F.Set[*Task]
		subTasksWg sync.WaitGroup

		name string

		OnFinishedFuncs []func()
		OnFinishedMu    sync.Mutex
		onFinishedWg    sync.WaitGroup

		finishOnce sync.Once
	}
)

var (
	ErrProgramExiting = errors.New("program exiting")
	ErrTaskCanceled   = errors.New("task canceled")

	logger = logging.With().Str("module", "task").Logger()
)

// GlobalTask returns a new Task with the given name, derived from the global context.
func GlobalTask(format string, args ...any) *Task {
	if len(args) > 0 {
		format = fmt.Sprintf(format, args...)
	}
	return globalTask.Subtask(format)
}

// DebugTaskMap returns a map[string]any representation of the global task tree.
//
// The returned map is suitable for encoding to JSON, and can be used
// to debug the task tree.
//
// The returned map is not guaranteed to be stable, and may change
// between runs of the program. It is intended for debugging purposes
// only.
func DebugTaskMap() map[string]any {
	return globalTask.serialize()
}

// CancelGlobalContext cancels the global task context, which will cause all tasks
// created to be canceled. This should be called before exiting the program
// to ensure that all tasks are properly cleaned up.
func CancelGlobalContext() {
	globalTask.cancel(ErrProgramExiting)
}

// GlobalContextWait waits for all tasks to finish, up to the given timeout.
//
// If the timeout is exceeded, it prints a list of all tasks that were
// still running when the timeout was reached, and their current tree
// of subtasks.
func GlobalContextWait(timeout time.Duration) (err error) {
	done := make(chan struct{})
	after := time.After(timeout)
	go func() {
		globalTask.Wait()
		close(done)
	}()
	for {
		select {
		case <-done:
			return
		case <-after:
			logger.Warn().Msg("Timeout waiting for these tasks to finish:\n" + globalTask.tree())
			return context.DeadlineExceeded
		}
	}
}

func (t *Task) trace(msg string) {
	logger.Trace().Str("name", t.name).Msg(msg)
}

// Name returns the name of the task.
func (t *Task) Name() string {
	if !common.IsTrace {
		return t.name
	}
	parts := strings.Split(t.name, " > ")
	return parts[len(parts)-1]
}

// String returns the name of the task.
func (t *Task) String() string {
	return t.name
}

// Context returns the context associated with the task. This context is
// canceled when Finish of the task is called, or parent task is canceled.
func (t *Task) Context() context.Context {
	return t.ctx
}

// FinishCause returns the reason / error that caused the task to be finished.
func (t *Task) FinishCause() error {
	cause := context.Cause(t.ctx)
	if cause == nil {
		return t.ctx.Err()
	}
	return cause
}

// Parent returns the parent task of the current task.
func (t *Task) Parent() *Task {
	return t.parent
}

func (t *Task) runAllOnFinished(onCompTask *Task) {
	<-t.ctx.Done()
	t.WaitSubTasks()
	for _, OnFinishedFunc := range t.OnFinishedFuncs {
		OnFinishedFunc()
		t.onFinishedWg.Done()
	}
	onCompTask.Finish(fmt.Errorf("%w: %s, reason: %s", ErrTaskCanceled, t.name, "done"))
}

// OnFinished calls fn when all subtasks are finished.
//
// It cannot be called after Finish or Wait is called.
func (t *Task) OnFinished(about string, fn func()) {
	if t.parent == globalTask {
		t.OnCancel(about, fn)
		return
	}
	t.onFinishedWg.Add(1)
	t.OnFinishedMu.Lock()
	defer t.OnFinishedMu.Unlock()

	if t.OnFinishedFuncs == nil {
		onCompTask := GlobalTask(t.name + " > OnFinished > " + about)
		go t.runAllOnFinished(onCompTask)
	}
	idx := len(t.OnFinishedFuncs)
	wrapped := func() {
		defer func() {
			if err := recover(); err != nil {
				logger.Error().
					Str("name", t.name).
					Interface("err", err).
					Msg("panic in " + about)
			}
		}()
		fn()
		logger.Trace().Str("name", t.name).Msgf("OnFinished[%d] done: %s", idx, about)
	}
	t.OnFinishedFuncs = append(t.OnFinishedFuncs, wrapped)
}

// OnCancel calls fn when the task is canceled.
//
// It cannot be called after Finish or Wait is called.
func (t *Task) OnCancel(about string, fn func()) {
	onCompTask := GlobalTask(t.name + " > OnFinished")
	go func() {
		<-t.ctx.Done()
		fn()
		onCompTask.Finish("done")
		t.trace("onCancel done: " + about)
	}()
}

// Finish marks the task as finished and cancel its context.
//
// Then call Wait to wait for all subtasks, OnFinished and OnSubtasksFinished
// of the task to finish.
//
// Note that it will also cancel all subtasks.
func (t *Task) Finish(reason any) {
	var format string
	switch reason.(type) {
	case error:
		format = "%w"
	case string, fmt.Stringer:
		format = "%s"
	default:
		format = "%v"
	}
	t.finishOnce.Do(func() {
		t.cancel(fmt.Errorf("%w: %s, reason: "+format, ErrTaskCanceled, t.name, reason))
	})
	t.Wait()
}

// Subtask returns a new subtask with the given name, derived from the parent's context.
//
// If the parent's context is already canceled, the returned subtask will be canceled immediately.
//
// This should not be called after Finish, Wait, or WaitSubTasks is called.
func (t *Task) Subtask(name string) *Task {
	ctx, cancel := context.WithCancelCause(t.ctx)
	return t.newSubTask(ctx, cancel, name)
}

func (t *Task) newSubTask(ctx context.Context, cancel context.CancelCauseFunc, name string) *Task {
	parent := t
	if common.IsTrace {
		name = parent.name + " > " + name
	}
	subtask := &Task{
		ctx:      ctx,
		cancel:   cancel,
		name:     name,
		parent:   parent,
		subtasks: F.NewSet[*Task](),
	}
	parent.subTasksWg.Add(1)
	parent.subtasks.Add(subtask)
	if common.IsTrace {
		subtask.trace("started")
		go func() {
			subtask.Wait()
			subtask.trace("finished: " + subtask.FinishCause().Error())
		}()
	}
	go func() {
		subtask.Wait()
		parent.subtasks.Remove(subtask)
		parent.subTasksWg.Done()
	}()
	return subtask
}

// Wait waits for all subtasks, itself, OnFinished and OnSubtasksFinished to finish.
//
// It must be called only after Finish is called.
func (t *Task) Wait() {
	<-t.ctx.Done()
	t.WaitSubTasks()
	t.onFinishedWg.Wait()
}

// WaitSubTasks waits for all subtasks of the task to finish.
//
// No more subtasks can be added after this call.
//
// It can be called before Finish is called.
func (t *Task) WaitSubTasks() {
	t.subTasksWg.Wait()
}

// tree returns a string representation of the task tree, with the given
// prefix prepended to each line. The prefix is used to indent the tree,
// and should be a string of spaces or a similar separator.
//
// The resulting string is suitable for printing to the console, and can be
// used to debug the task tree.
//
// The tree is traversed in a depth-first manner, with each task's name and
// line number (if available) printed on a separate line. The line number is
// only printed if the task was created with a non-empty line argument.
//
// The returned string is not guaranteed to be stable, and may change between
// runs of the program. It is intended for debugging purposes only.
func (t *Task) tree(prefix ...string) string {
	var sb strings.Builder
	var pre string
	if len(prefix) > 0 {
		pre = prefix[0]
		sb.WriteString(pre + "- ")
	}
	sb.WriteString(t.Name() + "\n")
	t.subtasks.RangeAll(func(subtask *Task) {
		sb.WriteString(subtask.tree(pre + "  "))
	})
	return sb.String()
}

// serialize returns a map[string]any representation of the task tree.
//
// The map contains the following keys:
// - name: the name of the task
// - subtasks: a slice of maps, each representing a subtask
//
// The subtask maps contain the same keys, recursively.
//
// The returned map is suitable for encoding to JSON, and can be used
// to debug the task tree.
//
// The returned map is not guaranteed to be stable, and may change
// between runs of the program. It is intended for debugging purposes
// only.
func (t *Task) serialize() map[string]any {
	m := make(map[string]any)
	parts := strings.Split(t.name, " > ")
	m["name"] = parts[len(parts)-1]
	if t.subtasks.Size() > 0 {
		m["subtasks"] = make([]map[string]any, 0, t.subtasks.Size())
		t.subtasks.RangeAll(func(subtask *Task) {
			m["subtasks"] = append(m["subtasks"].([]map[string]any), subtask.serialize())
		})
	}
	return m
}

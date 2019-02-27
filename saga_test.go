package saga

import (
	"context"
	"errors"
	"github.com/stretchr/testify/require"
	"testing"
)

type mock struct {
	callCounter int
	err         error
}

func (t *mock) f(ctx context.Context) error {
	t.callCounter++
	return t.err
}

func TestSuccessfullyExecTwoSteps(t *testing.T) {
	s := NewSaga("err4")

	m := &mock{}
	m2 := &mock{}
	comp := &mock{}

	require.NoError(t, s.AddStep(&Step{Name: "first", Func: m.f, CompensateFunc: comp.f}))
	require.NoError(t, s.AddStep(&Step{Name: "second", Func: m2.f, CompensateFunc: comp.f}))

	c := NewCoordinator(context.Background(), s, New())
	require.Nil(t, c.Play().ExecutionError)

	require.Equal(t, m.callCounter, 1)
	require.Equal(t, m2.callCounter, 1)
	require.Equal(t, comp.callCounter, 0)
}

func TestCompensateCalledWhenError(t *testing.T) {
	s := NewSaga("err3")

	m := &mock{err: errors.New("hello")}
	comp := &mock{}

	require.NoError(t, s.AddStep(&Step{Name: "single", Func: m.f, CompensateFunc: comp.f}))

	c := NewCoordinator(context.Background(), s, New())
	require.Error(t, c.Play().ExecutionError)

	require.Equal(t, m.callCounter, 1)
	require.Equal(t, comp.callCounter, 1)
}

func TestCompensateCalledTwiceForTwoSteps(t *testing.T) {
	s := NewSaga("err2")

	m := &mock{}
	comp := &mock{}
	m2 := &mock{err: errors.New("hello")}

	require.NoError(t, s.AddStep(&Step{Name: "first", Func: m.f, CompensateFunc: comp.f}))
	require.NoError(t, s.AddStep(&Step{Name: "second", Func: m2.f, CompensateFunc: comp.f}))

	c := NewCoordinator(context.Background(), s, New())
	c.Play()

	require.Equal(t, m.callCounter, 1)
	require.Equal(t, m2.callCounter, 1)
	require.Equal(t, comp.callCounter, 2)
}

func TestCompensateOnlyExecutedSteps(t *testing.T) {
	s := NewSaga("hello")

	m := &mock{err: errors.New("hello")}
	comp := &mock{}
	m2 := &mock{}

	require.NoError(t, s.AddStep(&Step{Name: "first", Func: m.f, CompensateFunc: comp.f}))
	require.NoError(t, s.AddStep(&Step{Name: "second", Func: m2.f, CompensateFunc: comp.f}))

	c := NewCoordinator(context.Background(), s, New())
	c.Play()

	require.Equal(t, m.callCounter, 1)
	require.Equal(t, m2.callCounter, 0)
	require.Equal(t, comp.callCounter, 1)
}

func TestReturnsError(t *testing.T) {
	s := NewSaga("hello")

	callCount1 := 0
	callCount2 := 0

	f1 := func(ctx context.Context) (string, error) {
		callCount1++
		return "hello", errors.New("some error")
	}
	f2 := func(ctx context.Context, s string) error {
		callCount2++
		require.Equal(t, "hello", s)
		return nil
	}

	require.NoError(t, s.AddStep(&Step{Name: "first", Func: f1, CompensateFunc: f2}))

	c := NewCoordinator(context.Background(), s, New())
	err := c.Play()

	require.EqualError(t, err.ExecutionError, "some error")
	require.Equal(t, callCount1, 1)
	require.Equal(t, callCount2, 1)
}

func TestCompensateReturnsError(t *testing.T) {
	s := NewSaga("hello")

	errFunc := func(ctx context.Context) error {
		return errors.New("some error")
	}
	errCompensateFirst := func(ctx context.Context) error {
		return errors.New("compensate error 1")
	}
	errCompensateSecond := func(ctx context.Context) error {
		return errors.New("compensate error 2")
	}

	require.NoError(t, s.AddStep(&Step{Name: "first", Func: (&mock{}).f, CompensateFunc: errCompensateFirst}))
	require.NoError(t, s.AddStep(&Step{Name: "second", Func: errFunc, CompensateFunc: errCompensateSecond}))

	logStore := New()
	c := NewCoordinator(context.Background(), s, logStore)
	result := c.Play()

	require.EqualError(t, result.ExecutionError, "some error")
	require.Len(t, result.CompensateErrors, 2)
	require.EqualError(t, result.CompensateErrors[0], "compensate error 2")
	require.EqualError(t, result.CompensateErrors[1], "compensate error 1")

	logs, err := logStore.GetAllLogsByExecutionID(c.ExecutionID)
	require.NoError(t, err)
	require.Len(t, logs, 7)
	require.Equal(t, logs[0].Type, LogTypeStartSaga)
	require.Equal(t, logs[1].Type, LogTypeSagaStepExec)
	require.Equal(t, logs[2].Type, LogTypeSagaStepExec)
	require.Equal(t, logs[3].Type, LogTypeSagaAbort)
	require.Equal(t, logs[4].Type, LogTypeSagaStepCompensate)
	require.Equal(t, logs[5].Type, LogTypeSagaStepCompensate)
	require.Equal(t, logs[6].Type, LogTypeSagaComplete)

	_, err = logStore.GetAllLogsByExecutionID(RandString())
	require.Error(t, err)
}

func TestAddStep(t *testing.T) {
	s := NewSaga("hello")

	require.EqualError(t, s.AddStep(&Step{Name: "first", Func: "hello", CompensateFunc: (&mock{}).f}), "func field is not a func, but string")
	require.EqualError(t, s.AddStep(&Step{Name: "first", Func: (&mock{}).f, CompensateFunc: 25}), "func field is not a func, but int")
	require.EqualError(t, s.AddStep(&Step{Name: "first", Func: func() {}, CompensateFunc: (&mock{}).f}), "func must have at least one parameter context.Context")
	require.EqualError(t, s.AddStep(&Step{Name: "first", Func: func(c int) {}, CompensateFunc: (&mock{}).f}), "first parameter of a func must be of type context.Context")
	require.EqualError(t, s.AddStep(&Step{Name: "first", Func: func(ctx context.Context) {}, CompensateFunc: (&mock{}).f}), "func must have at least one out value of type error")
	require.EqualError(t, s.AddStep(&Step{Name: "first", Func: func(context.Context) int { return 10 }, CompensateFunc: (&mock{}).f}), "last out parameter of func must be of type error")

	require.EqualError(t, s.AddStep(&Step{Name: "first", Func: (&mock{}).f, CompensateFunc: func() {}}), "compensate must have at least one parameter context.Context")
	require.EqualError(t, s.AddStep(&Step{Name: "first", Func: (&mock{}).f, CompensateFunc: func(int) {}}), "first parameter of a compensate must be of type context.Context")
	require.EqualError(t, s.AddStep(&Step{Name: "first", Func: (&mock{}).f, CompensateFunc: func(context.Context) {}}), "compensate must must return single value of type error")

	require.Panics(t, func() {
		checkOK(false)
	})
	require.Panics(t, func() {
		checkErr(errors.New("hello"))
	})
}

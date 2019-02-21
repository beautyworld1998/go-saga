package saga

import (
	"context"
	"errors"
	llog "log"
	"math/rand"
	"reflect"
	"time"
)

func NewSaga(ctx context.Context, name string, store Store) *Saga {
	return &Saga{
		ctx:         ctx,
		Name:        name,
		ExecutionID: RandString(),
		logStore:    store,
	}
}

type StepOptions struct {
}

type Step struct {
	Name           string
	Func           interface{}
	CompensateFunc interface{}
	Options        *StepOptions
}

type Result struct {
	Err error
}

type Saga struct {
	ExecutionID string
	Name        string

	returnedValuesFromFunc [][]reflect.Value
	toCompensate           []reflect.Value
	aborted                bool
	err                    error

	steps []*Step

	ctx context.Context

	logStore Store
}

func (saga *Saga) Play() *Result {
	checkErr(saga.logStore.AppendLog(&Log{
		ExecutionID: saga.ExecutionID,
		Name:        saga.Name,
		Time:        time.Now(),
		Type:        LogTypeStartSaga,
	}))

	for i := 0; i < len(saga.steps); i++ {
		saga.execStep(i)
	}

	checkErr(saga.logStore.AppendLog(&Log{
		ExecutionID: saga.ExecutionID,
		Name:        saga.Name,
		Time:        time.Now(),
		Type:        LogTypeSagaComplete,
	}))
	return &Result{Err: saga.err}
}

func (saga *Saga) AddStep(step *Step) {
	// FIXME check that f and compensate are correct and return an error
	saga.steps = append(saga.steps, step)
}

func (saga *Saga) abort() {
	stepsToCompensate := len(saga.toCompensate)
	checkErr(saga.logStore.AppendLog(&Log{
		ExecutionID: saga.ExecutionID,
		Name:        saga.Name,
		Time:        time.Now(),
		Type:        LogTypeSagaAbort,
		StepNumber:  &stepsToCompensate,
	}))

	saga.aborted = true
	for i := stepsToCompensate - 1; i >= 0; i-- {
		saga.compensateStep(i)
	}
}

func (saga *Saga) compensateStep(i int) {
	checkErr(saga.logStore.AppendLog(&Log{
		ExecutionID: saga.ExecutionID,
		Name:        saga.Name,
		Time:        time.Now(),
		Type:        LogTypeSagaStepCompensate,
		StepNumber:  &i,
		StepName:    &saga.steps[i].Name,
	}))

	params := make([]reflect.Value, 0)
	params = append(params, reflect.ValueOf(saga.ctx))
	params = addParams(params, saga.returnedValuesFromFunc[i])
	compensateFunc := saga.toCompensate[i]
	res := compensateFunc.Call(params)
	if err := isReturnError(res); err != nil {
		panic(err)
	}
}

func (saga *Saga) execStep(i int) {
	if saga.aborted {
		return
	}

	checkErr(saga.logStore.AppendLog(&Log{
		ExecutionID: saga.ExecutionID,
		Name:        saga.Name,
		Time:        time.Now(),
		Type:        LogTypeSagaStepExec,
		StepNumber:  &i,
		StepName:    &saga.steps[i].Name,
	}))

	f := saga.steps[i].Func
	compensate := saga.steps[i].CompensateFunc

	params := []reflect.Value{reflect.ValueOf(saga.ctx)}
	resp := getFuncValue(f).Call(params)

	saga.toCompensate = append(saga.toCompensate, getFuncValue(compensate))
	saga.returnedValuesFromFunc = append(saga.returnedValuesFromFunc, resp)

	if err := isReturnError(resp); err != nil {
		saga.err = err
		saga.abort()
	}
}

func isReturnError(result []reflect.Value) error {
	if len(result) > 0 && !result[len(result)-1].IsNil() {
		return result[len(result)-1].Interface().(error)
	}
	return nil
}

func addParams(values []reflect.Value, returned []reflect.Value) []reflect.Value {
	if len(returned) > 1 { // expect that this is error
		for i := 0; i < len(returned)-1; i++ {
			values = append(values, returned[i])
		}
	}
	return values
}

func getFuncValue(obj interface{}) reflect.Value {
	funcValue := reflect.ValueOf(obj)
	if funcValue.Kind() != reflect.Func {
		checkErr(errors.New("registered object must be a func"))
	}
	if funcValue.Type().NumIn() < 1 ||
		funcValue.Type().In(0) != reflect.TypeOf((*context.Context)(nil)).Elem() {
		checkErr(errors.New("first argument must use context.ctx"))
	}
	return funcValue
}

func checkErr(err error, msg ...string) {
	if err != nil {
		if err != nil {
			llog.Panicln(msg, err)
		}
	}
}

// RandString simply generates random string of length n
func RandString() string {
	var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
	b := make([]rune, 10)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

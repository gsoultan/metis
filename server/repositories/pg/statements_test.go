package pg

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/gsoultan/metis/server/repositories/model"
	"github.com/gsoultan/storm"
	"github.com/gsoultan/storm/runtime"
)

// errReached is what the executor below answers: a statement that gets this
// far was let run.
var errReached = errors.New("reached the executor")

type reachedExecutor struct{}

func (reachedExecutor) Query(context.Context, string, []any) (runtime.Rows, error) {
	return nil, errReached
}

func (reachedExecutor) Exec(context.Context, string, []any) (int64, error) { return 0, errReached }

func (reachedExecutor) CopyFrom(context.Context, string, []string, runtime.CopySource) (int64, error) {
	return 0, errReached
}

func (reachedExecutor) Batch(context.Context, []runtime.BatchOp, func(int, runtime.Rows, int64, error) error) error {
	return errReached
}

// Every statement the model declares reaches the database from this package.
// storm refuses one whose text was not registered at generate time, and the
// registration is in the store's root package, which nothing else here
// imports: without it every one of these failed at its first call.
func TestEveryDeclaredStatementRunsFromTheRepositories(t *testing.T) {
	for _, decl := range model.Queries() {
		_, sql := storm.DeclOf(decl)
		args := make([]reflect.Value, 0, 2+storm.ArgsOf(decl))
		args = append(args, reflect.ValueOf(context.Background()), reflect.ValueOf(runtime.Executor(reachedExecutor{})))
		for range storm.ArgsOf(decl) {
			args = append(args, reflect.Zero(reflect.TypeFor[any]()))
		}
		method := "Query"
		if _, ok := decl.(*storm.SQLStmt); ok {
			method = "Exec"
		}
		out := reflect.ValueOf(decl).MethodByName(method).Call(args)
		err, _ := out[len(out)-1].Interface().(error)
		if !errors.Is(err, errReached) {
			t.Errorf("%s did not reach the database: %v\n%s", method, err, sql)
		}
	}
}

package types

import (
	"fmt"
	tmlog "github.com/tendermint/tendermint/libs/log"
	"reflect"
	"regexp"
	"runtime"
	"time"
)

// Go lacks a simple and safe way to see if something is a typed nil.
// See:
//  - https://dave.cheney.net/2017/08/09/typed-nils-in-go-2
//  - https://groups.google.com/forum/#!topic/golang-nuts/wnH302gBa4I/discussion
//  - https://github.com/golang/go/issues/21538
func isTypedNil(o interface{}) bool {
	rv := reflect.ValueOf(o)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Map, reflect.Ptr, reflect.Slice:
		return rv.IsNil()
	default:
		return false
	}
}

// Returns true if it has zero length.
func isEmpty(o interface{}) bool {
	rv := reflect.ValueOf(o)
	switch rv.Kind() {
	case reflect.Array, reflect.Chan, reflect.Map, reflect.Slice, reflect.String:
		return rv.Len() == 0
	default:
		return false
	}
}

func TimeTrack(start time.Time, log tmlog.Logger) {
	elapsed := time.Since(start)

	// Skip this function, and fetch the PC and file for its parent.
	pc, _, _, _ := runtime.Caller(1)

	// Retrieve a function object this functions parent.
	funcObj := runtime.FuncForPC(pc)

	// Regex to extract just the function name (and not the module path).
	runtimeFunc := regexp.MustCompile(`^.*\.(.*)$`)
	name := runtimeFunc.ReplaceAllString(funcObj.Name(), "$1")

	if log == nil {
		log = tmlog.NewNopLogger()
	}
	log.Debug(fmt.Sprintf("%s took %s", name, elapsed))
}

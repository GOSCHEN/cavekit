package mux

import "context"

// ctx is a short helper used by mux_test.go so individual test cases do not
// repeat context.Background() boilerplate.
func ctx() context.Context {
	return context.Background()
}

package common

// ActionErr is our custom error type that includes a message and an action.
type ActionErr struct {
	err  error
	name string
}

func NewActionErr(err error, name string) *ActionErr {
	return &ActionErr{
		err:  err,
		name: name,
	}
}

func (e ActionErr) Error() string {
	return e.err.Error()
}

func (e ActionErr) Name() string {
	return e.name
}

type ErrorHandler interface {
	ShouldExit() bool
}

type StopOnError struct{}

func (s StopOnError) ShouldExit() bool { return true }

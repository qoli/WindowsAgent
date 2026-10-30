package windowsinput

// Error preserves the owning input failure code and stage across adapters.
type Error struct {
	Code  string
	Stage string
	Cause error
}

func (e *Error) Error() string {
	if e == nil || e.Cause == nil {
		return "Windows input failed"
	}
	return e.Cause.Error()
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

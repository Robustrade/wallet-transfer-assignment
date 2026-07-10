package utils

type CustomError struct {
	StatusCode int
	Message    string
}

func (e *CustomError) Error() string {
	return e.Message
}

func NewCustomError(statusCode int, message string) error {
	return &CustomError{StatusCode: statusCode, Message: message}
}

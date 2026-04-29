package response

// Success wraps a successful API response.
type Success struct {
	Status  int         `json:"status"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// Error wraps an error API response.
type Error struct {
	Status  int    `json:"status"`
	Message string `json:"message"`
}

func OK(message string, data interface{}) *Success {
	return &Success{Status: 200, Message: message, Data: data}
}

func Created(message string, data interface{}) *Success {
	return &Success{Status: 201, Message: message, Data: data}
}

func Err(status int, message string) *Error {
	return &Error{Status: status, Message: message}
}

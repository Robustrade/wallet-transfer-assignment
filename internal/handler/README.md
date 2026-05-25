# Handler Layer

HTTP handlers belong here.

Responsibilities:

- decode and validate HTTP requests
- call the service layer
- map service results/errors to HTTP responses

Handlers should stay thin. Business logic should live in `internal/service`.

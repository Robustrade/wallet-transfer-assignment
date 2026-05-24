# Repository Layer

Database access belongs here.

Responsibilities:

- execute SQL queries
- manage persistence models
- expose transaction-aware methods to the service layer

Workflow decisions should live in `internal/service`, not here.

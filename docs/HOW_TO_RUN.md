## Running Locally with Docker Compose

The Drauna backend service runs on port **8080** by default. You can access the API at `http://localhost:8080` when running locally.

Use docker compose to bring all the services up:

```bash
docker compose up -d
```

This command will build and start all containers in the background. To view logs for a specific service, use:

```bash
docker compose logs <service_name>
```

To stop all running containers:

```bash
docker compose down
```

## Tests

Automated tests are located in the `tests` directory. To run all tests:

```bash
go test ./...
```

## Database Migrations

Database migration files are stored in the `migrations` directory. To apply migrations, we use [goose](https://github.com/pressly/goose):

Migrations are automatically run when you do `docker compose up -d`. However to explicitly run just the migrations you can run following:
```bash
docker compose run --rm -it migrator
```

To create a new migration file run:
```bash
docker compose run --rm -it migrator create <your_migration_name>
```
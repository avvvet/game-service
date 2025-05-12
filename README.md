
# Install golang-migrate with PostgreSQL Tags:
go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest

# Create the Migration Files

```
migrate create -ext sql -dir internal/gamesvc/migrations create_users_table
```

## Apply migrations
```
migrate -path internal/gamesvc/migrations -database "postgres://postgres:password@localhost:5432/db?sslmode=disable" up
```

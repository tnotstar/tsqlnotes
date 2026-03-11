# tsqlnotes

## Overview
`tsqlnotes` is a high-performance, Go-based web application designed to serve as a notes management system with multi-database backend capabilities. The architecture relies extensively on the Go Standard Library for HTTP handling and data serving, prioritizing minimal external dependencies, type safety, and maintainability.

## Technology Stack
- **Language:** Go 1.26+
- **Database Compatibility:** Supports multiple relational databases through dedicated drivers:
  - PostgreSQL (`github.com/lib/pq`)
  - MySQL (`github.com/go-sql-driver/mysql`)
  - Microsoft SQL Server (`github.com/microsoft/go-mssqldb`)
  - SQLite (CGo-free via `modernc.org/sqlite`)
- **Configuration:** Environment variable management via `godotenv`.
- **Frontend Delivery:** Standard library-based HTML template rendering and static file serving.

## Prerequisites
- Go >= 1.26.1
- Access to a documented, supported database system (or standard filesystem access for SQLite).

## Setup & Execution
The project employs a standard `Makefile` to orchestrate build, run, and maintenance tasks.

### 1. Dependency Resolution
Resolve necessary module dependencies:
```bash
make deps
```

### 2. Configuration
The application relies on environment variables for configuration. Create a `.env` file at the root of the project to supply your connection strings and configuration parameters (e.g., listening port, target database driver).

### 3. Build the Application
Compile the executable:
```bash
make build
```
The resulting binary will be output to `bin/tsqlnotes`.

### 4. Run the Server
Launch the application directly from source:
```bash
make run
```
Or execute the compiled binary:
```bash
./bin/tsqlnotes
```

## Development Workflow
The repository requires adherence to strict idiomatic Go coding standards. The following `make` targets facilitate compliance:

- `make fmt`: Applies standard `gofmt` rules across the codebase.
- `make vet`: Executes static analysis via `go vet` to identify potential logic or structural errors.
- `make tidy`: Ensures the `go.mod` file accurately reflects dependencies via `go mod tidy`.
- `make test`: Executes the unit testing suite.
- `make clean`: Removes the `bin/` directory and standalone build artifacts.

## Architectural Principles
This software is developed following rigorous software engineering guidelines:
- **Minimal Dependencies:** Prioritizes the Standard Library. External packages are integrated only if they are highly consolidated and essential for database connectivity.
- **Interface Segregation:** Adheres to the "accept interfaces, return structs" methodology to maximize decoupling and testability.
- **Explicit Error Handling:** Enforces idiomatic Go error processing without ignoring error returns.

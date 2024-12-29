# Testing Guide

This directory contains various tests for the CDN service.

## Test Types

### 1. Unit Tests
Located in `unit/` directory, these test individual components in isolation.
```bash
go test ./test/unit/... -v
```

### 2. Integration Tests
Located in `integration/` directory, these test API endpoints with real HTTP calls.
```bash
go test ./test/integration/... -v
```

### 3. Load Tests
Located in `performance/` directory, using k6 for load testing.
```bash
k6 run test/performance/load_test.js
```

## Prerequisites

- Go 1.21 or higher
- k6 for load testing
- Docker for integration tests
- `testify` package for assertions

## Running Tests

### All Tests
```bash
make test
```

### Unit Tests with Coverage
```bash
go test ./test/unit/... -coverprofile=coverage.out
go tool cover -html=coverage.out
```

### Load Testing Scenarios

1. Basic Load Test:
```bash
k6 run test/performance/load_test.js
```

2. Stress Test (modify options in script):
```bash
k6 run --vus 50 --duration 5m test/performance/load_test.js
```

## Test Data

- Sample test files are in `test/data/`
- Mock services are in respective test directories
- Environment variables for tests in `.env.test` 
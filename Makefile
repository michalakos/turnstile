proto:
	protoc --go_out=. --go_opt=module=github.com/michalakos/turnstile \
	       --go-grpc_out=. --go-grpc_opt=module=github.com/michalakos/turnstile \
	       proto/ratelimiter.proto

test:
	go test ./...

test-integration:
	go test -tags integration ./internal/integration/...

loadtest-baseline:
	mkdir -p loadtest/results
	ghz --config loadtest/baseline.json --output loadtest/results/baseline.json

loadtest-saturation:
	mkdir -p loadtest/results
	ghz --config loadtest/saturation.json --output loadtest/results/saturation.json

.PHONY: proto test test-integration loadtest-baseline loadtest-saturation

proto:                                                       
	protoc --go_out=. --go_opt=module=github.com/michalakos/turnstile \
	       --go-grpc_out=. --go-grpc_opt=module=github.com/michalakos/turnstile \
	       proto/ratelimiter.proto

.PHONY: proto

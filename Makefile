.PHONY: check compatibility fmt release-parity verify verify-release

check:
	go run ./internal/qualitygate -mode=check

compatibility:
	go run ./internal/qualitygate -mode=compatibility

fmt:
	go run ./internal/qualitygate -mode=fmt

release-parity: export GOWORK := off
release-parity: export GOPROXY := off
release-parity: export GOTOOLCHAIN := local
release-parity: export GOFLAGS := -mod=vendor
release-parity:
	go run ./internal/qualitygate -mode=release-parity

verify:
	go run ./internal/qualitygate -mode=verify

verify-release:
	go run ./internal/qualitygate -mode=verify-release

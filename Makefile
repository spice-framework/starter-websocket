.PHONY: check compatibility fmt release-rehearsal verify verify-release

check:
	go run ./internal/qualitygate -mode=check

compatibility:
	go run ./internal/qualitygate -mode=compatibility

fmt:
	go run ./internal/qualitygate -mode=fmt

release-rehearsal: export GOWORK := off
release-rehearsal: export GOPROXY := off
release-rehearsal: export GOTOOLCHAIN := local
release-rehearsal: export GOFLAGS := -mod=vendor
release-rehearsal:
	go run ./internal/qualitygate -mode=release-rehearsal

verify:
	go run ./internal/qualitygate -mode=verify

verify-release:
	go run ./internal/qualitygate -mode=verify-release

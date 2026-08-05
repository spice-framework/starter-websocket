.PHONY: check compatibility fmt verify

check:
	go run ./internal/qualitygate -mode=check

compatibility:
	go run ./internal/qualitygate -mode=compatibility

fmt:
	go run ./internal/qualitygate -mode=fmt

verify:
	go run ./internal/qualitygate -mode=verify


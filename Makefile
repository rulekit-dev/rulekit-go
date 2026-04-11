.PHONY: test vet lint tag

test:
	go test ./...

vet:
	go vet ./...

lint:
	go vet ./...
	staticcheck ./...

tag:
	@if [ -z "$(v)" ]; then echo "usage: make tag v=0.1.0"; exit 1; fi
	git tag v$(v)
	git push origin v$(v)

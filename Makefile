dev:
	go build .

LDFLAGS := -X main.Version=v$(VERSION)
release:
	@echo "Checking that VERSION was defined in the calling environment"
	@test -n "$(VERSION)"
	@echo "OK.  VERSION=$(VERSION)"
	mkdir artifacts
	GOOS=linux  GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o artifacts/boss-linux-amd64  .
	GOOS=darwin GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o artifacts/boss-darwin-amd64 .

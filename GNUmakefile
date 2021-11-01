default: testacc

OS_TARGET = $(shell go env GOOS)_$(shell go env GOARCH)

build-local:
	mkdir -p ~/.terraform.d/plugins/akselleirv/local/gitlabcommit/0.0.1/$(OS_TARGET) \
	&& go build -o terraform-provider-gitlabcommit \
	&& mv terraform-provider-gitlabcommit ~/.terraform.d/plugins/akselleirv/local/gitlabcommit/0.0.1/$(OS_TARGET)

# Run acceptance tests
.PHONY: testacc
testacc:
	TF_ACC=1 go test ./... -v $(TESTARGS) -timeout 120m

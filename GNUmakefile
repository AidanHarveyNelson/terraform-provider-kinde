default: test

LOCAL_PROVIDER_DIR = $(CURDIR)/.tmp
LOCAL_PROVIDER_BINARY = $(LOCAL_PROVIDER_DIR)/terraform-provider-kinde
LOCAL_PROVIDER_TFRC = $(LOCAL_PROVIDER_DIR)/terraform-dev.tfrc
EXAMPLE ?= examples/resources/kinde_role
PROVIDER_SOURCE ?= kinde-oss/kinde
RESOURCE_EXAMPLES := $(sort $(wildcard examples/resources/*))
FEATURE_FLAG_TESTS = Environment|FeatureFlag

# Run acceptance tests
.PHONY: testacc
testacc:
	set -a && . ./.env && set +a && TF_ACC=1 go test ./... -v $(TESTARGS) -timeout 120m

.PHONY: test-feature-flags
test-feature-flags:
	go test ./internal/provider/ -v -count=1 -run '$(FEATURE_FLAG_TESTS)'

.PHONY: testacc-feature-flags
testacc-feature-flags:
	set -a && . ./.env && set +a && TF_ACC=1 go test ./internal/provider/ -v -count=1 -run '$(FEATURE_FLAG_TESTS)' -timeout 30m

# Run unit tests
.PHONY: test
test:
	go test ./... -v $(TESTARGS) -timeout 120s -parallel=4

# Run go fmt against code
.PHONY: fmt
fmt:
	gofmt -w -s .

# Run go vet against code
.PHONY: vet
vet:
	go vet ./...

# Generate documentation
.PHONY: docs
docs:
	tfplugindocs generate --provider-name kinde

# Validate documentation
.PHONY: docs-validate
docs-validate:
	tfplugindocs validate --provider-name kinde

# Run golangci-lint
.PHONY: lint
lint:
	golangci-lint run ./...

# Build provider
.PHONY: build
build: docs-validate
	go build -v ./...

# Install provider
.PHONY: install
install: build
	go install -v ./...

# Run a manual Terraform smoke test against a local provider binary.
.PHONY: example-smoke
example-smoke:
	mkdir -p "$(LOCAL_PROVIDER_DIR)"
	go build -o "$(LOCAL_PROVIDER_BINARY)" .
	printf 'provider_installation {\n  dev_overrides {\n    "%s" = "%s"\n  }\n  direct {}\n}\n' "$(PROVIDER_SOURCE)" "$(LOCAL_PROVIDER_DIR)" > "$(LOCAL_PROVIDER_TFRC)"
	rm -rf "$(EXAMPLE)/.terraform" "$(EXAMPLE)/.terraform.lock.hcl" "$(EXAMPLE)/terraform.tfstate" "$(EXAMPLE)/terraform.tfstate.backup"
	set -a && . ./.env && set +a && TF_CLI_CONFIG_FILE="$(LOCAL_PROVIDER_TFRC)" terraform -chdir="$(EXAMPLE)" init -backend=false
	set -a && . ./.env && set +a && TF_CLI_CONFIG_FILE="$(LOCAL_PROVIDER_TFRC)" terraform -chdir="$(EXAMPLE)" validate
	set -a && . ./.env && set +a && TF_CLI_CONFIG_FILE="$(LOCAL_PROVIDER_TFRC)" terraform -chdir="$(EXAMPLE)" plan -input=false
	set -a && . ./.env && set +a && TF_CLI_CONFIG_FILE="$(LOCAL_PROVIDER_TFRC)" terraform -chdir="$(EXAMPLE)" apply -input=false -auto-approve
	set -a && . ./.env && set +a && TF_CLI_CONFIG_FILE="$(LOCAL_PROVIDER_TFRC)" terraform -chdir="$(EXAMPLE)" plan -input=false
	set -a && . ./.env && set +a && TF_CLI_CONFIG_FILE="$(LOCAL_PROVIDER_TFRC)" terraform -chdir="$(EXAMPLE)" destroy -input=false -auto-approve
	rm -rf "$(EXAMPLE)/.terraform" "$(EXAMPLE)/.terraform.lock.hcl" "$(EXAMPLE)/terraform.tfstate" "$(EXAMPLE)/terraform.tfstate.backup"

# Run the local-provider smoke test against every resource example module.
.PHONY: example-smoke-all
example-smoke-all:
	for example in $(RESOURCE_EXAMPLES); do \
		$(MAKE) example-smoke EXAMPLE="$$example" || exit 1; \
	done

# Clean build artifacts
.PHONY: clean
clean:
	go clean -i ./...

# Run all pre-commit checks
.PHONY: all
all: fmt vet lint docs-validate test

# Version detection
VERSION = $(shell git describe --tags --match 'v*' 2>/dev/null || echo "v0.0.0")

# Test coverage
.PHONY: coverage
coverage:
	go test ./... -coverprofile=coverage.out
	go tool cover -html=coverage.out

# Note: Test resources are automatically cleaned up by the test framework
# at the end of each test. However, if tests fail or are interrupted,
# some resources might remain. These should be manually cleaned up in
# your Kinde account.

.PHONY: default build clean coverage docs docs-validate all example-smoke example-smoke-all

##@ Main
help:  ## display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m\033[0m\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

build: ## build application
	go build -C ./app -o ../streamrecorder

local: ## run local
	go run ./app --stream 'http://localhost:8000/stream.mp3' --port 8080 --dir ./records

clean: ## clean records
	rm -rf ./records

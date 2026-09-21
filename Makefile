BINARY := agl
INSTALL_PATH := /opt/amberguard/agl

# v8.3.0 (5.1): метаданные сборки внедряются на этапе компиляции через -ldflags.
# VERSION можно переопределить снаружи без правки файла:
#   make build VERSION=v8.3.1
VERSION ?= v8.4.2
# UTC-метка момента сборки (RFC3339); вычисляется при каждом запуске make,
# поэтому две сборки никогда не совпадут по BuildTime.
BUILD_TIME := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
# -X инжектит строку в переменную пакета internal/buildinfo:
#   buildinfo.Version   <- $(VERSION)
#   buildinfo.BuildTime <- $(BUILD_TIME)
# Без этих флагов (голый go build) бинарник честно покажет dev/unknown —
# это маркер нерелизной сборки, а не скрытая подмена версии.
LDFLAGS := -X agl/internal/buildinfo.Version=$(VERSION) \
           -X agl/internal/buildinfo.BuildTime=$(BUILD_TIME)

.PHONY: build vet fmt test install

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

vet:
	go vet ./...

fmt:
	gofmt -w .

test:
	go test ./...

install: build
	cp $(BINARY) $(INSTALL_PATH)